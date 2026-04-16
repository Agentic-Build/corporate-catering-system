use std::collections::BTreeMap;
use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use hmac::{Hmac, Mac};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

type HmacSha256 = Hmac<Sha256>;

const S3_SCHEME_PREFIX: &str = "s3://";
const MAX_OBJECT_REFERENCE_LENGTH: usize = 1024;
const MAX_SAFE_FILE_NAME_LENGTH: usize = 128;
const SIGV4_ALGORITHM: &str = "AWS4-HMAC-SHA256";
const MAX_PRESIGN_EXPIRY_SECONDS: u32 = 60 * 60;
const DEFAULT_UPLOAD_TTL_SECONDS: u32 = 15 * 60;
const DEFAULT_DOWNLOAD_TTL_SECONDS: u32 = 10 * 60;
static OBJECT_KEY_UNIQUENESS_SEQUENCE: AtomicU64 = AtomicU64::new(1);

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum StorageArtifactClass {
    MenuImage,
    MenuImageThumbnail,
    ComplianceDocument,
    FulfillmentDailySummary,
    FulfillmentPlantPartitionSheet,
    FulfillmentLabels,
    FulfillmentBasketList,
}

impl StorageArtifactClass {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::MenuImage => "MENU_IMAGE",
            Self::MenuImageThumbnail => "MENU_IMAGE_THUMBNAIL",
            Self::ComplianceDocument => "COMPLIANCE_DOCUMENT",
            Self::FulfillmentDailySummary => "FULFILLMENT_DAILY_SUMMARY",
            Self::FulfillmentPlantPartitionSheet => "FULFILLMENT_PLANT_PARTITION_SHEET",
            Self::FulfillmentLabels => "FULFILLMENT_LABELS",
            Self::FulfillmentBasketList => "FULFILLMENT_BASKET_LIST",
        }
    }

    pub const fn object_key_prefix(self) -> &'static str {
        match self {
            Self::MenuImage | Self::MenuImageThumbnail => "menu-images",
            Self::ComplianceDocument => "compliance-documents",
            Self::FulfillmentDailySummary
            | Self::FulfillmentPlantPartitionSheet
            | Self::FulfillmentLabels
            | Self::FulfillmentBasketList => "fulfillment-artifacts",
        }
    }

    const fn policy(self) -> StorageArtifactPolicy {
        match self {
            Self::MenuImage => StorageArtifactPolicy {
                max_size_bytes: 10 * 1024 * 1024,
                allowed_mime_types: &["image/jpeg", "image/png", "image/webp"],
                bucket: BucketTarget::Menu,
                include_thumbnail_plan: true,
            },
            Self::MenuImageThumbnail => StorageArtifactPolicy {
                max_size_bytes: 2 * 1024 * 1024,
                allowed_mime_types: &["image/jpeg", "image/png", "image/webp"],
                bucket: BucketTarget::Menu,
                include_thumbnail_plan: false,
            },
            Self::ComplianceDocument => StorageArtifactPolicy {
                max_size_bytes: 20 * 1024 * 1024,
                allowed_mime_types: &["application/pdf", "image/jpeg", "image/png"],
                bucket: BucketTarget::Compliance,
                include_thumbnail_plan: false,
            },
            Self::FulfillmentDailySummary
            | Self::FulfillmentPlantPartitionSheet
            | Self::FulfillmentLabels
            | Self::FulfillmentBasketList => StorageArtifactPolicy {
                max_size_bytes: 8 * 1024 * 1024,
                allowed_mime_types: &["application/json", "text/csv", "application/pdf"],
                bucket: BucketTarget::Fulfillment,
                include_thumbnail_plan: false,
            },
        }
    }
}

impl fmt::Display for StorageArtifactClass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StorageLocale {
    EnUs,
    ZhTw,
}

impl StorageLocale {
    pub fn from_language_tag(value: Option<&str>) -> Self {
        match value
            .unwrap_or_default()
            .trim()
            .to_ascii_lowercase()
            .as_str()
        {
            "zh" | "zh-tw" | "zh-hant" | "zh-hant-tw" => Self::ZhTw,
            _ => Self::EnUs,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ObjectStorageReference(String);

impl ObjectStorageReference {
    pub fn parse(value: impl Into<String>) -> Result<Self, ObjectStorageError> {
        let value = value.into();
        let trimmed = value.trim();
        if trimmed.is_empty() {
            return Err(ObjectStorageError::InvalidObjectReference(
                "object reference must not be empty".to_owned(),
            ));
        }
        if trimmed.len() > MAX_OBJECT_REFERENCE_LENGTH {
            return Err(ObjectStorageError::InvalidObjectReference(format!(
                "object reference must be at most {MAX_OBJECT_REFERENCE_LENGTH} characters"
            )));
        }
        let (bucket, key) = parse_object_reference_parts(trimmed)
            .map_err(|message| ObjectStorageError::InvalidObjectReference(message.to_owned()))?;
        validate_bucket_name(bucket)
            .map_err(|message| ObjectStorageError::InvalidObjectReference(message.to_owned()))?;
        validate_object_key(key)
            .map_err(|message| ObjectStorageError::InvalidObjectReference(message.to_owned()))?;
        Ok(Self(trimmed.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    pub fn split_parts(&self) -> (&str, &str) {
        parse_object_reference_parts(self.as_str())
            .expect("object reference should be valid after parsing")
    }
}

impl fmt::Display for ObjectStorageReference {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ObjectUploadIntent {
    pub artifact_class: StorageArtifactClass,
    pub owner_scope: Option<String>,
    pub file_name: String,
    pub mime_type: String,
    pub size_bytes: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ObjectUploadMetadata {
    pub artifact_class: StorageArtifactClass,
    pub file_name: String,
    pub mime_type: String,
    pub size_bytes: u64,
    pub thumbnail_ref: Option<ObjectStorageReference>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PresignedUploadTarget {
    pub object_ref: ObjectStorageReference,
    pub upload_url: String,
    pub upload_expires_at_epoch_seconds: i64,
    pub required_headers: BTreeMap<String, String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PresignedUploadPlan {
    pub primary: PresignedUploadTarget,
    pub thumbnail: Option<PresignedUploadTarget>,
    pub metadata: ObjectUploadMetadata,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PresignedDownloadPlan {
    pub object_ref: ObjectStorageReference,
    pub download_url: String,
    pub download_expires_at_epoch_seconds: i64,
}

#[derive(Debug, Clone)]
pub struct S3ObjectStorageConfig {
    endpoint: String,
    region: String,
    access_key_id: String,
    secret_access_key: String,
    menu_bucket: String,
    compliance_bucket: String,
    fulfillment_bucket: String,
    upload_ttl_seconds: u32,
    download_ttl_seconds: u32,
    key_namespace: String,
}

impl S3ObjectStorageConfig {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        endpoint: impl Into<String>,
        region: impl Into<String>,
        access_key_id: impl Into<String>,
        secret_access_key: impl Into<String>,
        menu_bucket: impl Into<String>,
        compliance_bucket: impl Into<String>,
        fulfillment_bucket: impl Into<String>,
    ) -> Result<Self, ObjectStorageError> {
        let config = Self {
            endpoint: normalize_endpoint(endpoint.into())?,
            region: normalize_non_empty(region.into(), "region")?,
            access_key_id: normalize_non_empty(access_key_id.into(), "access key id")?,
            secret_access_key: normalize_non_empty(secret_access_key.into(), "secret access key")?,
            menu_bucket: normalize_non_empty(menu_bucket.into(), "menu bucket")?,
            compliance_bucket: normalize_non_empty(compliance_bucket.into(), "compliance bucket")?,
            fulfillment_bucket: normalize_non_empty(
                fulfillment_bucket.into(),
                "fulfillment bucket",
            )?,
            upload_ttl_seconds: DEFAULT_UPLOAD_TTL_SECONDS,
            download_ttl_seconds: DEFAULT_DOWNLOAD_TTL_SECONDS,
            key_namespace: String::new(),
        };
        config.validate()?;
        Ok(config)
    }

    pub fn with_ttls(
        mut self,
        upload_ttl_seconds: u32,
        download_ttl_seconds: u32,
    ) -> Result<Self, ObjectStorageError> {
        self.upload_ttl_seconds = upload_ttl_seconds;
        self.download_ttl_seconds = download_ttl_seconds;
        self.validate()?;
        Ok(self)
    }

    pub fn with_key_namespace(mut self, key_namespace: impl Into<String>) -> Self {
        self.key_namespace = key_namespace.into().trim().to_owned();
        self
    }

    fn validate(&self) -> Result<(), ObjectStorageError> {
        validate_bucket_name(&self.menu_bucket).map_err(|message| {
            ObjectStorageError::InvalidConfiguration(format!("menu bucket is invalid: {message}"))
        })?;
        validate_bucket_name(&self.compliance_bucket).map_err(|message| {
            ObjectStorageError::InvalidConfiguration(format!(
                "compliance bucket is invalid: {message}"
            ))
        })?;
        validate_bucket_name(&self.fulfillment_bucket).map_err(|message| {
            ObjectStorageError::InvalidConfiguration(format!(
                "fulfillment bucket is invalid: {message}"
            ))
        })?;
        if self.upload_ttl_seconds == 0 || self.upload_ttl_seconds > MAX_PRESIGN_EXPIRY_SECONDS {
            return Err(ObjectStorageError::InvalidConfiguration(format!(
                "upload ttl seconds must be between 1 and {MAX_PRESIGN_EXPIRY_SECONDS}"
            )));
        }
        if self.download_ttl_seconds == 0 || self.download_ttl_seconds > MAX_PRESIGN_EXPIRY_SECONDS
        {
            return Err(ObjectStorageError::InvalidConfiguration(format!(
                "download ttl seconds must be between 1 and {MAX_PRESIGN_EXPIRY_SECONDS}"
            )));
        }
        Ok(())
    }
}

#[derive(Debug, Clone)]
pub struct ObjectStorageUploadPipeline {
    config: S3ObjectStorageConfig,
}

impl ObjectStorageUploadPipeline {
    pub fn new(config: S3ObjectStorageConfig) -> Result<Self, ObjectStorageError> {
        config.validate()?;
        Ok(Self { config })
    }

    pub fn create_upload_plan(
        &self,
        intent: ObjectUploadIntent,
        now: SystemTime,
    ) -> Result<PresignedUploadPlan, ObjectStorageError> {
        let policy = intent.artifact_class.policy();
        let normalized_mime = normalize_mime(intent.mime_type.as_str(), intent.artifact_class)?;
        if !policy
            .allowed_mime_types
            .contains(&normalized_mime.as_str())
        {
            return Err(ObjectStorageError::InvalidMimeType {
                artifact_class: intent.artifact_class,
                mime_type: normalized_mime,
            });
        }
        if intent.size_bytes == 0 || intent.size_bytes > policy.max_size_bytes {
            return Err(ObjectStorageError::SizeLimitExceeded {
                artifact_class: intent.artifact_class,
                size_bytes: intent.size_bytes,
                max_size_bytes: policy.max_size_bytes,
            });
        }

        let normalized_owner_scope = intent
            .owner_scope
            .as_deref()
            .map(normalize_owner_scope)
            .transpose()?;
        let normalized_file_name = normalize_file_name(intent.file_name.as_str())?;
        let (amz_timestamp, short_date, now_epoch_seconds) = format_amz_timestamp(now)?;
        let object_key = self.build_object_key(
            intent.artifact_class,
            normalized_owner_scope.as_deref(),
            &normalized_file_name,
            &normalized_mime,
            intent.size_bytes,
            now_epoch_seconds,
            &short_date,
            false,
        );
        let object_ref = self.make_reference(policy.bucket, &object_key)?;
        let mut primary_headers = BTreeMap::new();
        primary_headers.insert("content-type".to_owned(), normalized_mime.clone());
        primary_headers.insert(
            "x-amz-meta-artifact-class".to_owned(),
            intent.artifact_class.as_str().to_owned(),
        );
        primary_headers.insert(
            "x-amz-meta-original-file-name".to_owned(),
            normalized_file_name.clone(),
        );
        primary_headers.insert(
            "x-amz-meta-size-bytes".to_owned(),
            intent.size_bytes.to_string(),
        );
        primary_headers.insert("content-length".to_owned(), intent.size_bytes.to_string());
        let (upload_url, upload_expires_at_epoch_seconds) = self.presign_url(
            "PUT",
            &object_ref,
            self.config.upload_ttl_seconds,
            &amz_timestamp,
            &short_date,
            &primary_headers,
            now_epoch_seconds,
        )?;

        let mut metadata = ObjectUploadMetadata {
            artifact_class: intent.artifact_class,
            file_name: normalized_file_name,
            mime_type: normalized_mime,
            size_bytes: intent.size_bytes,
            thumbnail_ref: None,
        };

        let thumbnail = if policy.include_thumbnail_plan {
            let thumbnail_mime = "image/webp".to_owned();
            let thumbnail_key = self.build_object_key(
                StorageArtifactClass::MenuImageThumbnail,
                normalized_owner_scope.as_deref(),
                "thumbnail.webp",
                thumbnail_mime.as_str(),
                intent.size_bytes.min(
                    StorageArtifactClass::MenuImageThumbnail
                        .policy()
                        .max_size_bytes,
                ),
                now_epoch_seconds,
                &short_date,
                true,
            );
            let thumbnail_ref = self.make_reference(BucketTarget::Menu, &thumbnail_key)?;
            metadata.thumbnail_ref = Some(thumbnail_ref.clone());

            let mut thumbnail_headers = BTreeMap::new();
            thumbnail_headers.insert("content-type".to_owned(), thumbnail_mime);
            thumbnail_headers.insert(
                "x-amz-meta-artifact-class".to_owned(),
                StorageArtifactClass::MenuImageThumbnail.as_str().to_owned(),
            );
            thumbnail_headers.insert(
                "x-amz-meta-thumbnail-of".to_owned(),
                object_ref.as_str().to_owned(),
            );
            let (thumbnail_upload_url, thumbnail_upload_expires_at_epoch_seconds) = self
                .presign_url(
                    "PUT",
                    &thumbnail_ref,
                    self.config.upload_ttl_seconds,
                    &amz_timestamp,
                    &short_date,
                    &thumbnail_headers,
                    now_epoch_seconds,
                )?;

            Some(PresignedUploadTarget {
                object_ref: thumbnail_ref,
                upload_url: thumbnail_upload_url,
                upload_expires_at_epoch_seconds: thumbnail_upload_expires_at_epoch_seconds,
                required_headers: thumbnail_headers,
            })
        } else {
            None
        };

        Ok(PresignedUploadPlan {
            primary: PresignedUploadTarget {
                object_ref,
                upload_url,
                upload_expires_at_epoch_seconds,
                required_headers: primary_headers,
            },
            thumbnail,
            metadata,
        })
    }

    pub fn create_download_plan(
        &self,
        object_ref: &ObjectStorageReference,
        now: SystemTime,
    ) -> Result<PresignedDownloadPlan, ObjectStorageError> {
        let (bucket, _) = object_ref.split_parts();
        self.ensure_managed_bucket(bucket)?;
        let (amz_timestamp, short_date, now_epoch_seconds) = format_amz_timestamp(now)?;
        let (download_url, download_expires_at_epoch_seconds) = self.presign_url(
            "GET",
            object_ref,
            self.config.download_ttl_seconds,
            &amz_timestamp,
            &short_date,
            &BTreeMap::new(),
            now_epoch_seconds,
        )?;
        Ok(PresignedDownloadPlan {
            object_ref: object_ref.clone(),
            download_url,
            download_expires_at_epoch_seconds,
        })
    }

    fn ensure_managed_bucket(&self, bucket: &str) -> Result<(), ObjectStorageError> {
        if bucket == self.config.menu_bucket.as_str()
            || bucket == self.config.compliance_bucket.as_str()
            || bucket == self.config.fulfillment_bucket.as_str()
        {
            return Ok(());
        }
        Err(ObjectStorageError::InvalidObjectReference(format!(
            "bucket `{bucket}` is not managed by this runtime"
        )))
    }

    pub fn ensure_managed_reference(
        &self,
        object_ref: &ObjectStorageReference,
    ) -> Result<(), ObjectStorageError> {
        let (bucket, _) = object_ref.split_parts();
        self.ensure_managed_bucket(bucket)
    }

    fn make_reference(
        &self,
        bucket_target: BucketTarget,
        object_key: &str,
    ) -> Result<ObjectStorageReference, ObjectStorageError> {
        let bucket = match bucket_target {
            BucketTarget::Menu => self.config.menu_bucket.as_str(),
            BucketTarget::Compliance => self.config.compliance_bucket.as_str(),
            BucketTarget::Fulfillment => self.config.fulfillment_bucket.as_str(),
        };
        ObjectStorageReference::parse(format!("{S3_SCHEME_PREFIX}{bucket}/{object_key}"))
    }

    fn build_object_key(
        &self,
        artifact_class: StorageArtifactClass,
        owner_scope: Option<&str>,
        file_name: &str,
        mime_type: &str,
        size_bytes: u64,
        now_epoch_seconds: i64,
        short_date: &str,
        is_thumbnail: bool,
    ) -> String {
        let uniqueness_sequence = OBJECT_KEY_UNIQUENESS_SEQUENCE.fetch_add(1, Ordering::Relaxed);
        let mut digest = Sha256::new();
        digest.update(artifact_class.as_str().as_bytes());
        if let Some(owner_scope) = owner_scope {
            digest.update(owner_scope.as_bytes());
        }
        digest.update(file_name.as_bytes());
        digest.update(mime_type.as_bytes());
        digest.update(size_bytes.to_string().as_bytes());
        digest.update(now_epoch_seconds.to_string().as_bytes());
        digest.update(uniqueness_sequence.to_be_bytes());
        let digest_hex = hex_encode(&digest.finalize()[..8]);
        let file_name = if is_thumbnail {
            "thumbnail.webp".to_owned()
        } else {
            ensure_file_extension(file_name, mime_type)
        };
        let namespace = normalize_namespace(self.config.key_namespace.as_str());
        let base_prefix = if namespace.is_empty() {
            artifact_class.object_key_prefix().to_owned()
        } else {
            format!("{}/{}", namespace, artifact_class.object_key_prefix())
        };
        match owner_scope {
            Some(owner_scope) => {
                format!(
                    "{base_prefix}/{owner_scope}/{short_date}/{size_bytes}-{digest_hex}-{file_name}"
                )
            }
            None => format!("{base_prefix}/{short_date}/{size_bytes}-{digest_hex}-{file_name}"),
        }
    }

    #[allow(clippy::too_many_arguments)]
    fn presign_url(
        &self,
        method: &str,
        object_ref: &ObjectStorageReference,
        expires_seconds: u32,
        amz_timestamp: &str,
        short_date: &str,
        additional_headers: &BTreeMap<String, String>,
        now_epoch_seconds: i64,
    ) -> Result<(String, i64), ObjectStorageError> {
        let endpoint = parse_endpoint(self.config.endpoint.as_str())?;
        let (bucket, key) = object_ref.split_parts();
        let canonical_uri = build_canonical_uri(endpoint.base_path.as_str(), bucket, key);
        let mut canonical_header_map = BTreeMap::new();
        canonical_header_map.insert("host".to_owned(), endpoint.host.clone());
        for (header_name, header_value) in additional_headers {
            canonical_header_map.insert(
                header_name.trim().to_ascii_lowercase(),
                header_value.trim().to_owned(),
            );
        }

        let signed_headers = canonical_header_map
            .keys()
            .cloned()
            .collect::<Vec<_>>()
            .join(";");
        let canonical_headers = canonical_header_map
            .iter()
            .map(|(name, value)| format!("{name}:{value}\n"))
            .collect::<String>();

        let credential_scope = format!("{short_date}/{}/s3/aws4_request", self.config.region);
        let credential = format!("{}/{}", self.config.access_key_id, credential_scope);
        let mut query_parameters = vec![
            ("X-Amz-Algorithm".to_owned(), SIGV4_ALGORITHM.to_owned()),
            ("X-Amz-Credential".to_owned(), credential),
            ("X-Amz-Date".to_owned(), amz_timestamp.to_owned()),
            ("X-Amz-Expires".to_owned(), expires_seconds.to_string()),
            ("X-Amz-SignedHeaders".to_owned(), signed_headers.clone()),
        ];
        query_parameters.sort();
        let canonical_query_string = query_parameters
            .iter()
            .map(|(key, value)| {
                format!(
                    "{}={}",
                    percent_encode(key.as_str(), true),
                    percent_encode(value.as_str(), true)
                )
            })
            .collect::<Vec<_>>()
            .join("&");

        let canonical_request = format!(
            "{method}\n{canonical_uri}\n{canonical_query_string}\n{canonical_headers}\n{signed_headers}\nUNSIGNED-PAYLOAD"
        );
        let canonical_request_hash = sha256_hex(canonical_request.as_bytes());
        let string_to_sign = format!(
            "{SIGV4_ALGORITHM}\n{amz_timestamp}\n{credential_scope}\n{canonical_request_hash}"
        );
        let signature = self.sign_string(short_date, string_to_sign.as_str())?;
        let final_query = format!("{canonical_query_string}&X-Amz-Signature={signature}");
        let final_url = format!(
            "{}://{}{}?{}",
            endpoint.scheme, endpoint.host, canonical_uri, final_query
        );
        Ok((
            final_url,
            now_epoch_seconds.saturating_add(i64::from(expires_seconds)),
        ))
    }

    fn sign_string(
        &self,
        short_date: &str,
        string_to_sign: &str,
    ) -> Result<String, ObjectStorageError> {
        let k_date = hmac_sha256(
            format!("AWS4{}", self.config.secret_access_key).as_bytes(),
            short_date.as_bytes(),
        )?;
        let k_region = hmac_sha256(&k_date, self.config.region.as_bytes())?;
        let k_service = hmac_sha256(&k_region, b"s3")?;
        let k_signing = hmac_sha256(&k_service, b"aws4_request")?;
        let signature = hmac_sha256(&k_signing, string_to_sign.as_bytes())?;
        Ok(hex_encode(signature.as_slice()))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ObjectStorageError {
    InvalidObjectReference(String),
    InvalidMimeType {
        artifact_class: StorageArtifactClass,
        mime_type: String,
    },
    SizeLimitExceeded {
        artifact_class: StorageArtifactClass,
        size_bytes: u64,
        max_size_bytes: u64,
    },
    InvalidFileName(String),
    InvalidConfiguration(String),
    PresignFailed(String),
}

impl ObjectStorageError {
    pub const fn error_code(&self) -> &'static str {
        match self {
            Self::InvalidObjectReference(_) => "OBJECT_STORAGE_INVALID_OBJECT_REF",
            Self::InvalidMimeType { .. } => "OBJECT_STORAGE_INVALID_MIME",
            Self::SizeLimitExceeded { .. } => "OBJECT_STORAGE_SIZE_EXCEEDED",
            Self::InvalidFileName(_) => "OBJECT_STORAGE_INVALID_FILE_NAME",
            Self::InvalidConfiguration(_) => "OBJECT_STORAGE_CONFIGURATION_ERROR",
            Self::PresignFailed(_) => "OBJECT_STORAGE_PRESIGN_ERROR",
        }
    }

    pub fn localized_message(&self, locale: StorageLocale) -> String {
        match locale {
            StorageLocale::EnUs => match self {
                Self::InvalidObjectReference(message)
                | Self::InvalidFileName(message)
                | Self::InvalidConfiguration(message)
                | Self::PresignFailed(message) => message.clone(),
                Self::InvalidMimeType {
                    artifact_class,
                    mime_type,
                } => format!(
                    "MIME type `{mime_type}` is not allowed for artifact class `{artifact_class}`"
                ),
                Self::SizeLimitExceeded {
                    artifact_class,
                    size_bytes,
                    max_size_bytes,
                } => format!(
                    "Payload size {size_bytes} bytes exceeds `{artifact_class}` limit {max_size_bytes} bytes"
                ),
            },
            StorageLocale::ZhTw => match self {
                Self::InvalidObjectReference(_) => "物件參考格式錯誤，必須使用 s3://bucket/key。".to_owned(),
                Self::InvalidFileName(_) => "檔名格式錯誤，請使用英數與 .-_ 字元。".to_owned(),
                Self::InvalidConfiguration(_) => "物件儲存設定錯誤，請聯絡系統管理員。".to_owned(),
                Self::PresignFailed(_) => "無法產生上傳或下載授權連結，請稍後再試。".to_owned(),
                Self::InvalidMimeType {
                    artifact_class,
                    mime_type,
                } => format!(
                    "檔案 MIME 類型 `{mime_type}` 不支援 `{artifact_class}`。"
                ),
                Self::SizeLimitExceeded {
                    artifact_class,
                    size_bytes,
                    max_size_bytes,
                } => format!(
                    "檔案大小 {size_bytes} bytes 超過 `{artifact_class}` 上限 {max_size_bytes} bytes。"
                ),
            },
        }
    }
}

impl fmt::Display for ObjectStorageError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.localized_message(StorageLocale::EnUs).as_str())
    }
}

impl std::error::Error for ObjectStorageError {}

#[derive(Debug, Clone, Copy)]
struct StorageArtifactPolicy {
    max_size_bytes: u64,
    allowed_mime_types: &'static [&'static str],
    bucket: BucketTarget,
    include_thumbnail_plan: bool,
}

#[derive(Debug, Clone, Copy)]
enum BucketTarget {
    Menu,
    Compliance,
    Fulfillment,
}

#[derive(Debug, Clone)]
struct ParsedEndpoint {
    scheme: String,
    host: String,
    base_path: String,
}

fn parse_endpoint(value: &str) -> Result<ParsedEndpoint, ObjectStorageError> {
    let normalized = normalize_endpoint(value.to_owned())?;
    let (scheme, rest) = normalized.split_once("://").ok_or_else(|| {
        ObjectStorageError::InvalidConfiguration("endpoint must include scheme".to_owned())
    })?;
    let (host, path) = match rest.split_once('/') {
        Some((host, path)) => (host.trim(), path.trim_matches('/')),
        None => (rest.trim(), ""),
    };
    if host.is_empty() {
        return Err(ObjectStorageError::InvalidConfiguration(
            "endpoint host must not be empty".to_owned(),
        ));
    }
    let base_path = if path.is_empty() {
        String::new()
    } else {
        format!("/{}", path)
    };
    Ok(ParsedEndpoint {
        scheme: scheme.to_owned(),
        host: host.to_owned(),
        base_path,
    })
}

fn normalize_endpoint(value: String) -> Result<String, ObjectStorageError> {
    let trimmed = value.trim().trim_end_matches('/').to_owned();
    if trimmed.is_empty() {
        return Err(ObjectStorageError::InvalidConfiguration(
            "endpoint must not be empty".to_owned(),
        ));
    }
    if trimmed.contains("://") {
        return Ok(trimmed);
    }
    Ok(format!("http://{trimmed}"))
}

fn normalize_non_empty(value: String, field: &str) -> Result<String, ObjectStorageError> {
    let trimmed = value.trim().to_owned();
    if trimmed.is_empty() {
        return Err(ObjectStorageError::InvalidConfiguration(format!(
            "{field} must not be empty"
        )));
    }
    Ok(trimmed)
}

fn normalize_mime(
    value: &str,
    artifact_class: StorageArtifactClass,
) -> Result<String, ObjectStorageError> {
    let normalized = value.trim().to_ascii_lowercase();
    if normalized.is_empty() {
        return Err(ObjectStorageError::InvalidMimeType {
            artifact_class,
            mime_type: String::new(),
        });
    }
    if normalized
        .bytes()
        .any(|byte| !(byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'/' || byte == b'.'))
    {
        return Err(ObjectStorageError::InvalidMimeType {
            artifact_class,
            mime_type: normalized,
        });
    }
    Ok(normalized)
}

fn parse_object_reference_parts(value: &str) -> Result<(&str, &str), &'static str> {
    let Some(payload) = value.strip_prefix(S3_SCHEME_PREFIX) else {
        return Err("object reference must start with s3://");
    };
    let Some((bucket, key)) = payload.split_once('/') else {
        return Err("object reference must include bucket and key");
    };
    if bucket.trim().is_empty() {
        return Err("bucket segment must not be empty");
    }
    if key.trim().is_empty() {
        return Err("object key segment must not be empty");
    }
    Ok((bucket, key))
}

fn validate_bucket_name(value: &str) -> Result<(), &'static str> {
    if value.len() < 3 || value.len() > 63 {
        return Err("bucket length must be between 3 and 63 characters");
    }
    let bytes = value.as_bytes();
    if !bytes[0].is_ascii_lowercase() && !bytes[0].is_ascii_digit() {
        return Err("bucket must start with lowercase letter or digit");
    }
    if !bytes[bytes.len() - 1].is_ascii_lowercase() && !bytes[bytes.len() - 1].is_ascii_digit() {
        return Err("bucket must end with lowercase letter or digit");
    }
    if bytes.iter().any(|byte| {
        !(byte.is_ascii_lowercase() || byte.is_ascii_digit() || *byte == b'.' || *byte == b'-')
    }) {
        return Err("bucket can contain only lowercase letters, digits, dots, and hyphens");
    }
    Ok(())
}

fn validate_object_key(value: &str) -> Result<(), &'static str> {
    if value.starts_with('/') || value.ends_with('/') {
        return Err("object key must not start or end with slash");
    }
    if value.contains("//") {
        return Err("object key must not contain consecutive slashes");
    }
    if value.chars().any(char::is_whitespace) {
        return Err("object key must not contain whitespace");
    }
    Ok(())
}

fn normalize_file_name(value: &str) -> Result<String, ObjectStorageError> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(ObjectStorageError::InvalidFileName(
            "file name must not be empty".to_owned(),
        ));
    }
    if trimmed.len() > MAX_SAFE_FILE_NAME_LENGTH {
        return Err(ObjectStorageError::InvalidFileName(format!(
            "file name must be at most {MAX_SAFE_FILE_NAME_LENGTH} characters"
        )));
    }
    let mut normalized = String::with_capacity(trimmed.len());
    for character in trimmed.chars() {
        if character.is_ascii_alphanumeric() || matches!(character, '-' | '_' | '.') {
            normalized.push(character.to_ascii_lowercase());
        } else {
            normalized.push('-');
        }
    }
    let normalized = normalized.trim_matches('-').to_owned();
    if normalized.is_empty() || normalized == "." || normalized == ".." {
        return Err(ObjectStorageError::InvalidFileName(
            "file name became empty after normalization".to_owned(),
        ));
    }
    Ok(normalized)
}

fn normalize_owner_scope(value: &str) -> Result<String, ObjectStorageError> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(ObjectStorageError::InvalidFileName(
            "owner scope must not be empty".to_owned(),
        ));
    }
    let mut normalized = String::with_capacity(trimmed.len());
    for character in trimmed.chars() {
        if character.is_ascii_alphanumeric() || matches!(character, '-' | '_' | '.') {
            normalized.push(character.to_ascii_lowercase());
        } else {
            normalized.push('-');
        }
    }
    let normalized = normalized.trim_matches('-').to_owned();
    if normalized.is_empty() || normalized == "." || normalized == ".." {
        return Err(ObjectStorageError::InvalidFileName(
            "owner scope became empty after normalization".to_owned(),
        ));
    }
    Ok(normalized)
}

fn ensure_file_extension(file_name: &str, mime_type: &str) -> String {
    let inferred_extension = match mime_type {
        "image/jpeg" => "jpg",
        "image/png" => "png",
        "image/webp" => "webp",
        "application/pdf" => "pdf",
        "text/csv" => "csv",
        _ => "json",
    };
    if file_name.rsplit('.').next().is_some_and(|segment| {
        !segment.is_empty()
            && segment
                .chars()
                .all(|character| character.is_ascii_alphanumeric())
    }) {
        file_name.to_owned()
    } else {
        format!("{file_name}.{inferred_extension}")
    }
}

fn normalize_namespace(value: &str) -> String {
    let trimmed = value.trim().trim_matches('/').to_owned();
    if trimmed.is_empty() {
        String::new()
    } else {
        trimmed
    }
}

fn format_amz_timestamp(now: SystemTime) -> Result<(String, String, i64), ObjectStorageError> {
    let duration = now.duration_since(UNIX_EPOCH).map_err(|_| {
        ObjectStorageError::PresignFailed("system clock is before Unix epoch".to_owned())
    })?;
    let epoch_seconds = i64::try_from(duration.as_secs()).map_err(|_| {
        ObjectStorageError::PresignFailed("epoch seconds overflowed i64 range".to_owned())
    })?;
    let epoch_day = epoch_seconds.div_euclid(86_400);
    let seconds_of_day = epoch_seconds.rem_euclid(86_400);
    let hour = seconds_of_day / 3_600;
    let minute = (seconds_of_day % 3_600) / 60;
    let second = seconds_of_day % 60;
    let (year, month, day) = civil_from_days(epoch_day);
    let short_date = format!("{year:04}{month:02}{day:02}");
    let amz_timestamp = format!("{short_date}T{hour:02}{minute:02}{second:02}Z");
    Ok((amz_timestamp, short_date, epoch_seconds))
}

fn build_canonical_uri(base_path: &str, bucket: &str, key: &str) -> String {
    let mut segments = Vec::new();
    if !base_path.is_empty() {
        segments.push(base_path.trim_matches('/').to_owned());
    }
    segments.push(bucket.to_owned());
    segments.push(percent_encode(key, false));
    format!("/{}", segments.join("/"))
}

fn percent_encode(value: &str, encode_slash: bool) -> String {
    let mut output = String::new();
    for byte in value.as_bytes() {
        if byte.is_ascii_alphanumeric() || matches!(*byte, b'-' | b'_' | b'.' | b'~') {
            output.push(char::from(*byte));
            continue;
        }
        if !encode_slash && *byte == b'/' {
            output.push('/');
            continue;
        }
        output.push('%');
        output.push_str(format!("{byte:02X}").as_str());
    }
    output
}

fn sha256_hex(value: &[u8]) -> String {
    let mut digest = Sha256::new();
    digest.update(value);
    hex_encode(&digest.finalize())
}

fn hmac_sha256(key: &[u8], value: &[u8]) -> Result<Vec<u8>, ObjectStorageError> {
    let mut mac = HmacSha256::new_from_slice(key)
        .map_err(|error| ObjectStorageError::PresignFailed(error.to_string()))?;
    mac.update(value);
    Ok(mac.finalize().into_bytes().to_vec())
}

fn hex_encode(value: &[u8]) -> String {
    let mut output = String::with_capacity(value.len() * 2);
    for byte in value {
        output.push_str(format!("{byte:02x}").as_str());
    }
    output
}

fn civil_from_days(days_since_epoch: i64) -> (i32, u32, u32) {
    let shifted_days = days_since_epoch + 719_468;
    let era = if shifted_days >= 0 {
        shifted_days
    } else {
        shifted_days - 146_096
    } / 146_097;
    let day_of_era = shifted_days - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_piece = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * month_piece + 2) / 5 + 1;
    let month = month_piece + if month_piece < 10 { 3 } else { -9 };
    let year = year + if month <= 2 { 1 } else { 0 };

    (
        i32::try_from(year).expect("civil year should fit in i32"),
        u32::try_from(month).expect("civil month should fit in u32"),
        u32::try_from(day).expect("civil day should fit in u32"),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pipeline() -> ObjectStorageUploadPipeline {
        let config = S3ObjectStorageConfig::new(
            "http://127.0.0.1:9000",
            "us-east-1",
            "minio-access",
            "minio-secret",
            "menu-assets",
            "compliance-docs",
            "fulfillment-exports",
        )
        .expect("config should be valid")
        .with_key_namespace("corporate-catering");
        ObjectStorageUploadPipeline::new(config).expect("pipeline should initialize")
    }

    #[test]
    fn object_reference_requires_s3_scheme() {
        assert!(ObjectStorageReference::parse("s3://bucket/path/file.pdf").is_ok());
        assert!(ObjectStorageReference::parse("https://bucket/path/file.pdf").is_err());
    }

    #[test]
    fn menu_image_upload_plan_contains_thumbnail_and_signed_url() {
        let pipeline = pipeline();
        let plan = pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class: StorageArtifactClass::MenuImage,
                    owner_scope: None,
                    file_name: "menu-photo.jpg".to_owned(),
                    mime_type: "image/jpeg".to_owned(),
                    size_bytes: 128_000,
                },
                UNIX_EPOCH + std::time::Duration::from_secs(1_712_000_000),
            )
            .expect("upload plan should be generated");
        assert!(plan
            .primary
            .upload_url
            .contains("X-Amz-Algorithm=AWS4-HMAC-SHA256"));
        assert!(plan.thumbnail.is_some());
        assert_eq!(
            plan.primary
                .required_headers
                .get("x-amz-meta-artifact-class"),
            Some(&"MENU_IMAGE".to_owned())
        );
        assert_eq!(
            plan.primary.required_headers.get("content-length"),
            Some(&"128000".to_owned())
        );
    }

    #[test]
    fn unsupported_mime_is_rejected_with_localized_error() {
        let pipeline = pipeline();
        let error = pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class: StorageArtifactClass::ComplianceDocument,
                    owner_scope: None,
                    file_name: "safety-cert.exe".to_owned(),
                    mime_type: "application/octet-stream".to_owned(),
                    size_bytes: 64,
                },
                SystemTime::now(),
            )
            .expect_err("unsupported mime should fail");
        assert_eq!(error.error_code(), "OBJECT_STORAGE_INVALID_MIME");
        assert!(error
            .localized_message(StorageLocale::ZhTw)
            .contains("不支援"));
    }

    #[test]
    fn size_limit_is_enforced_per_artifact_class() {
        let pipeline = pipeline();
        let error = pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class: StorageArtifactClass::MenuImage,
                    owner_scope: None,
                    file_name: "oversized-menu.jpg".to_owned(),
                    mime_type: "image/jpeg".to_owned(),
                    size_bytes: 11 * 1024 * 1024,
                },
                SystemTime::now(),
            )
            .expect_err("oversized payload should fail");
        assert_eq!(error.error_code(), "OBJECT_STORAGE_SIZE_EXCEEDED");
    }

    #[test]
    fn download_plan_uses_aws_sigv4_query_parameters() {
        let pipeline = pipeline();
        let reference = ObjectStorageReference::parse(
            "s3://menu-assets/corporate-catering/menu-images/day/test.jpg",
        )
        .expect("reference should parse");
        let plan = pipeline
            .create_download_plan(
                &reference,
                UNIX_EPOCH + std::time::Duration::from_secs(1_712_000_000),
            )
            .expect("download plan should succeed");
        assert!(plan
            .download_url
            .contains("X-Amz-Algorithm=AWS4-HMAC-SHA256"));
        assert!(plan.download_url.contains("X-Amz-Signature="));
    }

    #[test]
    fn download_plan_rejects_unmanaged_bucket_references() {
        let pipeline = pipeline();
        let reference = ObjectStorageReference::parse("s3://external-bucket/path/file.pdf")
            .expect("reference should parse");
        let error = pipeline
            .create_download_plan(&reference, SystemTime::now())
            .expect_err("unmanaged bucket should be rejected");
        assert_eq!(error.error_code(), "OBJECT_STORAGE_INVALID_OBJECT_REF");
    }

    #[test]
    fn upload_object_key_includes_uniqueness_sequence() {
        let pipeline = pipeline();
        let now = UNIX_EPOCH + std::time::Duration::from_secs(1_712_000_000);
        let plan_one = pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class: StorageArtifactClass::MenuImage,
                    owner_scope: None,
                    file_name: "menu-photo.jpg".to_owned(),
                    mime_type: "image/jpeg".to_owned(),
                    size_bytes: 128_000,
                },
                now,
            )
            .expect("first upload plan should be generated");
        let plan_two = pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class: StorageArtifactClass::MenuImage,
                    owner_scope: None,
                    file_name: "menu-photo.jpg".to_owned(),
                    mime_type: "image/jpeg".to_owned(),
                    size_bytes: 128_000,
                },
                now,
            )
            .expect("second upload plan should be generated");
        assert_ne!(
            plan_one.primary.object_ref.as_str(),
            plan_two.primary.object_ref.as_str(),
            "object refs must remain collision-safe under identical metadata and second-level timestamp"
        );
    }

    #[test]
    fn upload_object_key_includes_owner_scope_segment() {
        let pipeline = pipeline();
        let now = UNIX_EPOCH + std::time::Duration::from_secs(1_712_000_000);
        let plan = pipeline
            .create_upload_plan(
                ObjectUploadIntent {
                    artifact_class: StorageArtifactClass::ComplianceDocument,
                    owner_scope: Some("ven-load-gate-a".to_owned()),
                    file_name: "license.pdf".to_owned(),
                    mime_type: "application/pdf".to_owned(),
                    size_bytes: 65_536,
                },
                now,
            )
            .expect("upload plan should be generated");
        assert!(plan
            .primary
            .object_ref
            .as_str()
            .contains("/ven-load-gate-a/"));
    }
}
