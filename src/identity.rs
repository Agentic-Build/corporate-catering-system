use std::fmt;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Role {
    Employee,
    VendorOperator,
    CommitteeAdmin,
    PayrollOperator,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum AuthenticationSource {
    CorporateSso,
    VendorAccountMfa,
    OAuthServiceAccount,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum EmploymentStatus {
    Active,
    Terminated,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct ActorId(String);

impl ActorId {
    pub fn parse(value: impl Into<String>) -> Result<Self, IdentityContextError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(IdentityContextError::InvalidActorId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for ActorId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct PlantId(String);

impl PlantId {
    pub fn parse(value: impl Into<String>) -> Result<Self, IdentityContextError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(IdentityContextError::InvalidPlantId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for PlantId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum PlantScope {
    AllPlants,
    Restricted(Vec<PlantId>),
}

impl PlantScope {
    pub fn all() -> Self {
        Self::AllPlants
    }

    pub fn restricted(plants: Vec<PlantId>) -> Result<Self, IdentityContextError> {
        if plants.is_empty() {
            return Err(IdentityContextError::EmptyPlantScope);
        }
        Ok(Self::Restricted(plants))
    }

    pub fn contains(&self, plant_id: &PlantId) -> bool {
        match self {
            Self::AllPlants => true,
            Self::Restricted(plants) => plants.iter().any(|candidate| candidate == plant_id),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AuthenticatedActorContext {
    actor_id: ActorId,
    role: Role,
    plant_scope: PlantScope,
    authentication_source: AuthenticationSource,
    employment_status: EmploymentStatus,
}

impl AuthenticatedActorContext {
    pub fn new(
        actor_id: ActorId,
        role: Role,
        plant_scope: PlantScope,
        authentication_source: AuthenticationSource,
    ) -> Result<Self, IdentityContextError> {
        Self::new_with_employment_status(
            actor_id,
            role,
            plant_scope,
            authentication_source,
            EmploymentStatus::Active,
        )
    }

    pub fn new_with_employment_status(
        actor_id: ActorId,
        role: Role,
        plant_scope: PlantScope,
        authentication_source: AuthenticationSource,
        employment_status: EmploymentStatus,
    ) -> Result<Self, IdentityContextError> {
        validate_identity_source(role, authentication_source)?;
        validate_plant_scope(role, &plant_scope)?;

        Ok(Self {
            actor_id,
            role,
            plant_scope,
            authentication_source,
            employment_status,
        })
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn role(&self) -> Role {
        self.role
    }

    pub fn plant_scope(&self) -> &PlantScope {
        &self.plant_scope
    }

    pub fn authentication_source(&self) -> AuthenticationSource {
        self.authentication_source
    }

    pub fn employment_status(&self) -> EmploymentStatus {
        self.employment_status
    }
}

fn validate_identity_source(
    role: Role,
    source: AuthenticationSource,
) -> Result<(), IdentityContextError> {
    match (role, source) {
        (Role::Employee, AuthenticationSource::CorporateSso)
        | (Role::Employee, AuthenticationSource::OAuthServiceAccount)
        | (Role::CommitteeAdmin, AuthenticationSource::CorporateSso)
        | (Role::CommitteeAdmin, AuthenticationSource::OAuthServiceAccount)
        | (Role::PayrollOperator, AuthenticationSource::CorporateSso)
        | (Role::PayrollOperator, AuthenticationSource::OAuthServiceAccount)
        | (Role::VendorOperator, AuthenticationSource::VendorAccountMfa)
        | (Role::VendorOperator, AuthenticationSource::OAuthServiceAccount) => Ok(()),
        _ => Err(IdentityContextError::UnsupportedIdentitySource { role, source }),
    }
}

fn validate_plant_scope(role: Role, plant_scope: &PlantScope) -> Result<(), IdentityContextError> {
    match (role, plant_scope) {
        (Role::Employee, PlantScope::AllPlants) | (Role::VendorOperator, PlantScope::AllPlants) => {
            Err(IdentityContextError::RoleRequiresRestrictedPlantScope(role))
        }
        _ => Ok(()),
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IdentityContextError {
    InvalidActorId,
    InvalidPlantId,
    EmptyPlantScope,
    UnsupportedIdentitySource {
        role: Role,
        source: AuthenticationSource,
    },
    RoleRequiresRestrictedPlantScope(Role),
}

impl fmt::Display for IdentityContextError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidActorId => f.write_str("actor id must not be empty"),
            Self::InvalidPlantId => f.write_str("plant id must not be empty"),
            Self::EmptyPlantScope => {
                f.write_str("restricted plant scope must include at least one plant")
            }
            Self::UnsupportedIdentitySource { role, source } => {
                write!(
                    f,
                    "identity source {source:?} is not allowed for role {role:?}"
                )
            }
            Self::RoleRequiresRestrictedPlantScope(role) => {
                write!(f, "role {role:?} requires restricted plant scope")
            }
        }
    }
}

impl std::error::Error for IdentityContextError {}
