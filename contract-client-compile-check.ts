import { OpenAPI } from "./generated/ts-client";
import type {
  EmployeeOrder,
  MenuPage,
  PayrollDeductionPage,
  VendorEnrollmentPage,
} from "./generated/ts-client";

OpenAPI.BASE = "https://api.corporate-catering.example.com";

const sampleOrderId: EmployeeOrder["orderId"] = "ord-contractsmoke01";
void sampleOrderId;

export type ContractClientCompileCheck = {
  menu: MenuPage;
  enrollment: VendorEnrollmentPage;
  payroll: PayrollDeductionPage;
};
