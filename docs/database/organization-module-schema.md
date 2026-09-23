# Organization and tenant schema

Migration `000002_organization_multitenancy.sql` adds the SaaS and organization boundary without changing migration `000001`.

## Ownership model

- `platform.tenants` is the SaaS isolation boundary.
- `organization.companies` is the legal/payroll entity and belongs to one tenant.
- `organization.units` stores typed location, department, position, and group records.
- `employee.employees` now has immutable `tenant_id` and `company_id` ownership.
- `employee.employments` and `tax.employee_tax_profiles` retain tenant/company ownership and use composite foreign keys back to the employee owner.

Employee number and encrypted-NIK lookup uniqueness are scoped to `(tenant_id, company_id)`. Organization units are unique by `(tenant_id, company_id, unit_type, code)`.

Tenant/company/unit ownership changes are rejected by database triggers. Units and organization records are archive/status based; hard deletion is rejected. Employee, employment, assignment, and tax-profile history keeps the original ownership and remains protected by the guards inherited from the employee migration.

The migration deliberately fails when employee rows already exist. Historical employee data must be explicitly mapped to a tenant and company before enabling this boundary; an automatic guess would be unsafe for sensitive payroll and tax data.
