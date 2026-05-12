-- E2E seed: idempotent fixture for the Playwright employee login smoke test.
-- Pairs with the FakeProvider in services/api/internal/identity/oidc/fake.go
-- (enabled via FAKE_OIDC=1) which returns this same email.
INSERT INTO employee_directory (employee_id, primary_email, display_name, plant, department)
VALUES ('E2E001', 'e2e-employee@tbite.test', 'E2E 員工', 'F12B-3F', 'IT')
ON CONFLICT (employee_id) DO NOTHING;
