#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HealthProbeKind {
    Readiness,
    Liveness,
    Startup,
}

impl HealthProbeKind {
    pub const fn path(self) -> &'static str {
        match self {
            Self::Readiness => "/health/ready",
            Self::Liveness => "/health/live",
            Self::Startup => "/health/startup",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct RuntimeHealthRoute {
    probe_kind: HealthProbeKind,
    path: &'static str,
}

impl RuntimeHealthRoute {
    pub const fn new(probe_kind: HealthProbeKind, path: &'static str) -> Self {
        Self { probe_kind, path }
    }

    pub const fn probe_kind(self) -> HealthProbeKind {
        self.probe_kind
    }

    pub const fn path(self) -> &'static str {
        self.path
    }
}

const RUNTIME_HEALTH_ROUTES: [RuntimeHealthRoute; 3] = [
    RuntimeHealthRoute::new(HealthProbeKind::Readiness, HealthProbeKind::Readiness.path()),
    RuntimeHealthRoute::new(HealthProbeKind::Liveness, HealthProbeKind::Liveness.path()),
    RuntimeHealthRoute::new(HealthProbeKind::Startup, HealthProbeKind::Startup.path()),
];

pub fn runtime_health_routes() -> &'static [RuntimeHealthRoute] {
    &RUNTIME_HEALTH_ROUTES
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HealthState {
    Healthy,
    Unhealthy,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HealthReport {
    probe_kind: HealthProbeKind,
    state: HealthState,
    detail: String,
}

impl HealthReport {
    pub fn probe_kind(&self) -> HealthProbeKind {
        self.probe_kind
    }

    pub fn state(&self) -> HealthState {
        self.state
    }

    pub fn detail(&self) -> &str {
        &self.detail
    }
}

pub fn evaluate_probe(
    probe_kind: HealthProbeKind,
    dependencies_ready: bool,
    detail: impl Into<String>,
) -> HealthReport {
    let state = match probe_kind {
        HealthProbeKind::Liveness => HealthState::Healthy,
        HealthProbeKind::Readiness | HealthProbeKind::Startup => {
            if dependencies_ready {
                HealthState::Healthy
            } else {
                HealthState::Unhealthy
            }
        }
    };

    HealthReport {
        probe_kind,
        state,
        detail: detail.into(),
    }
}
