use std::time::{SystemTime, UNIX_EPOCH};

use hmac::{Hmac, Mac};
use sha2::Sha256;
use subtle::ConstantTimeEq;

use crate::menu_supply_window::OrderId;

const TAIPEI_FIXED_OFFSET_SECONDS: i64 = 8 * 60 * 60;
const TOTP_STEP_SECONDS: i64 = 30;
const TOTP_DIGITS: u32 = 6;
const TOTP_MODULO: u32 = 10_u32.pow(TOTP_DIGITS);
const ALLOWED_STEP_DRIFT: u64 = 1;
const QR_PREFIX: &str = "TOTP1";

type HmacSha256 = Hmac<Sha256>;

#[derive(Debug, Clone)]
pub struct PickupTotpVerifier {
    secret: Vec<u8>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VerifiedTotp {
    pub step: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PickupTotpVerificationError {
    InvalidFormat(&'static str),
    Expired { token_step: u64, current_step: u64 },
    NotYetValid { token_step: u64, current_step: u64 },
    InvalidCode,
}

impl PickupTotpVerificationError {
    pub const fn as_audit_reason(&self) -> &'static str {
        match self {
            Self::InvalidFormat(_) => "invalid-format",
            Self::Expired { .. } => "expired",
            Self::NotYetValid { .. } => "not-yet-valid",
            Self::InvalidCode => "invalid-code",
        }
    }
}

impl PickupTotpVerifier {
    pub fn from_secret(secret: impl Into<Vec<u8>>) -> Result<Self, String> {
        let secret = secret.into();
        if secret.is_empty() {
            return Err("pickup TOTP secret must be non-empty".to_owned());
        }
        Ok(Self { secret })
    }

    pub fn from_env(var_name: &str) -> Result<Self, String> {
        let secret = std::env::var(var_name)
            .map_err(|_| format!("{var_name} must be set for pickup TOTP verification"))?;
        if secret.trim().is_empty() {
            return Err(format!(
                "{var_name} must be non-empty for pickup TOTP verification"
            ));
        }
        Self::from_secret(secret.into_bytes())
    }

    pub fn current_taipei_step() -> Result<u64, String> {
        let unix_seconds = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|error| format!("failed to read system clock: {error}"))?
            .as_secs();
        let unix_seconds = i64::try_from(unix_seconds)
            .map_err(|_| "system clock overflowed i64 seconds".to_owned())?;
        taipei_step_from_unix_seconds(unix_seconds)
    }

    pub fn generate_qr_payload(&self, order_id: &OrderId, step: u64) -> String {
        let otp = self.generate_totp_code(order_id, step);
        format!("{QR_PREFIX}:{step}:{otp}")
    }

    pub fn verify(
        &self,
        order_id: &OrderId,
        verification_code: &str,
        current_step: u64,
    ) -> Result<VerifiedTotp, PickupTotpVerificationError> {
        let parsed = parse_qr_payload(verification_code)?;
        if parsed.step.saturating_add(ALLOWED_STEP_DRIFT) < current_step {
            return Err(PickupTotpVerificationError::Expired {
                token_step: parsed.step,
                current_step,
            });
        }
        if parsed.step > current_step.saturating_add(ALLOWED_STEP_DRIFT) {
            return Err(PickupTotpVerificationError::NotYetValid {
                token_step: parsed.step,
                current_step,
            });
        }

        let expected = self.generate_totp_code(order_id, parsed.step);
        if parsed.otp.as_bytes().ct_eq(expected.as_bytes()).unwrap_u8() != 1 {
            return Err(PickupTotpVerificationError::InvalidCode);
        }

        Ok(VerifiedTotp { step: parsed.step })
    }

    fn generate_totp_code(&self, order_id: &OrderId, step: u64) -> String {
        let mut mac = HmacSha256::new_from_slice(&self.secret)
            .expect("pickup TOTP secret is validated as non-empty");
        let message = format!("{}:{step}", order_id.as_str());
        mac.update(message.as_bytes());
        let digest = mac.finalize().into_bytes();

        let offset = (digest[digest.len() - 1] & 0x0f) as usize;
        let binary = (u32::from(digest[offset] & 0x7f) << 24)
            | (u32::from(digest[offset + 1]) << 16)
            | (u32::from(digest[offset + 2]) << 8)
            | u32::from(digest[offset + 3]);
        let otp = binary % TOTP_MODULO;
        format!("{otp:0width$}", width = TOTP_DIGITS as usize)
    }
}

pub fn taipei_step_from_unix_seconds(unix_seconds: i64) -> Result<u64, String> {
    let shifted_seconds = unix_seconds
        .checked_add(TAIPEI_FIXED_OFFSET_SECONDS)
        .ok_or_else(|| "pickup TOTP time conversion overflowed".to_owned())?;
    let step = shifted_seconds.div_euclid(TOTP_STEP_SECONDS);
    u64::try_from(step).map_err(|_| "pickup TOTP step underflowed unix epoch range".to_owned())
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ParsedQrPayload {
    step: u64,
    otp: String,
}

fn parse_qr_payload(raw: &str) -> Result<ParsedQrPayload, PickupTotpVerificationError> {
    let value = raw.trim();
    if value.is_empty() {
        return Err(PickupTotpVerificationError::InvalidFormat(
            "verificationCode must be non-empty",
        ));
    }

    let mut parts = value.split(':');
    let prefix = parts
        .next()
        .ok_or(PickupTotpVerificationError::InvalidFormat(
            "verificationCode is malformed",
        ))?;
    if prefix != QR_PREFIX {
        return Err(PickupTotpVerificationError::InvalidFormat(
            "verificationCode prefix must be `TOTP1`",
        ));
    }

    let step = parts
        .next()
        .ok_or(PickupTotpVerificationError::InvalidFormat(
            "verificationCode must include step segment",
        ))?
        .parse::<u64>()
        .map_err(|_| {
            PickupTotpVerificationError::InvalidFormat(
                "verificationCode step must be an unsigned integer",
            )
        })?;
    let otp = parts
        .next()
        .ok_or(PickupTotpVerificationError::InvalidFormat(
            "verificationCode must include otp segment",
        ))?;
    if parts.next().is_some() {
        return Err(PickupTotpVerificationError::InvalidFormat(
            "verificationCode must contain exactly 3 segments",
        ));
    }
    if otp.len() != TOTP_DIGITS as usize || !otp.chars().all(|ch| ch.is_ascii_digit()) {
        return Err(PickupTotpVerificationError::InvalidFormat(
            "verificationCode otp must be exactly 6 digits",
        ));
    }

    Ok(ParsedQrPayload {
        step,
        otp: otp.to_owned(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn order_id(value: &str) -> OrderId {
        OrderId::parse(value).expect("order id should be valid")
    }

    #[test]
    fn verifier_accepts_current_and_adjacent_steps() {
        let verifier =
            PickupTotpVerifier::from_secret("totp-test-secret".as_bytes()).expect("valid secret");
        let order_id = order_id("ord-aaaaaaaa");
        let current_step = 1_234_567;

        for step in [current_step - 1, current_step, current_step + 1] {
            let payload = verifier.generate_qr_payload(&order_id, step);
            let verified = verifier
                .verify(&order_id, &payload, current_step)
                .expect("step should be accepted");
            assert_eq!(verified.step, step);
        }
    }

    #[test]
    fn verifier_rejects_expired_step() {
        let verifier =
            PickupTotpVerifier::from_secret("totp-test-secret".as_bytes()).expect("valid secret");
        let order_id = order_id("ord-bbbbbbbb");
        let current_step = 50_000;
        let payload = verifier.generate_qr_payload(&order_id, current_step - 2);

        let error = verifier
            .verify(&order_id, &payload, current_step)
            .expect_err("expired code should be rejected");
        assert!(matches!(
            error,
            PickupTotpVerificationError::Expired {
                token_step: _,
                current_step: _
            }
        ));
    }

    #[test]
    fn verifier_rejects_code_bound_to_different_order() {
        let verifier =
            PickupTotpVerifier::from_secret("totp-test-secret".as_bytes()).expect("valid secret");
        let order_a = order_id("ord-cccccccc");
        let order_b = order_id("ord-dddddddd");
        let step = 42_000;
        let payload = verifier.generate_qr_payload(&order_a, step);

        let error = verifier
            .verify(&order_b, &payload, step)
            .expect_err("cross-order replay must be rejected");
        assert_eq!(error, PickupTotpVerificationError::InvalidCode);
    }

    #[test]
    fn taipei_step_uses_fixed_plus_8_offset_boundaries() {
        assert_eq!(
            taipei_step_from_unix_seconds(0).expect("step should be resolved"),
            960
        );
        assert_eq!(
            taipei_step_from_unix_seconds(30).expect("step should be resolved"),
            961
        );
    }
}
