# Organization API contract

The organization API establishes the multi-tenant SaaS boundary for Imogi.

## Resource model

```text
platform administrator
        |
        +-- Tenant
                |
                +-- Company (legal/payroll entity)
                        |
                        +-- Location
                        +-- Department
                        +-- Position
                        +-- Group
```

- A tenant is the SaaS isolation boundary.
- A tenant may contain multiple companies.
- Company is the owner of employee, employment, payroll, and tax scope.
- Organization units are typed, company-scoped, flat, and archive-only in V0.
- Tenant and company lifecycle records are never hard-deleted.

## Request context and authorization

Tenant context comes from a trusted token or server-side session context. A
client-supplied tenant ID, company ID, or header is not an authorization
mechanism. Company IDs in request bodies are validated against the active
tenant and the caller's company policy.

Authentication remains external OAuth2/OIDC. Tenant membership, fixed roles,
capabilities, and company scope are application-managed and will be added in
the IAM phase.

## Employee contract alignment

Employee identity is company-scoped. `CreateEmployeeRequest.companyId` is
required, and employee number/NIK uniqueness is intended to be scoped to the
company. Existing employment commands retain `companyId` for compatibility,
but the application must validate that it matches the employee owner company.

The current employee database migration predates this organization contract.
The tenant/company ownership columns and scoped constraints must be introduced
by a new migration after this API contract is approved; the existing migration
must not be edited.
