## Kingdee purchase orders

Fetches one page of purchase order lines from Kingdee K3Cloud through the `ExecuteBillQuery` WebAPI (`PUR_PurchaseOrder` form). The node signs each request with the third-party app credentials and returns a single page; the caller controls paging with `start_row` and `limit` and checks `has_more` to continue. Typically bound to a remote runner inside the network where the K3Cloud gateway is reachable.

### Secrets

- `app_secret` (`string`): Third-party app secret.

### Variables

- `server_url` (`string`): K3Cloud gateway URL, e.g. `https://erp.example.com/K3Cloud`.
- `acct_id` (`string`): Account book ID.
- `user_name` (`string`): Integration user name.
- `app_id` (`string`): Third-party app ID (the `xxxxxx_yyyy` value from the Kingdee developer console).
- `org_num` (`string`): Optional organization number for multi-organization deployments.
- `skip_tls_verify` (`string`): Set to `true` to accept self-signed gateway certificates. TLS is verified by default.

### Inputs

- `filter_string` (`string`): Optional Kingdee filter expression (SQL WHERE syntax), e.g. `FDocumentStatus='C' AND FDate>='2026-01-01'`; empty matches all rows. Any field key below can be referenced.
- `field_keys` (`string[]`): Optional Kingdee field keys to fetch, see the field reference below. When set, output records are keyed by the field keys themselves; when unset the 13 built-in default columns are used.
- `order_string` (`string`): Optional sort expression (SQL ORDER BY syntax), e.g. `FDate DESC, FID DESC` for newest first; defaults to `FID ASC`. Sort keys must be stable and unique when paging (always end with `FID` as a tiebreaker), otherwise pages can repeat or skip rows.
- `start_row` (`int`): Optional zero-based row offset to start the page at; defaults to 0.
- `limit` (`int`): Optional page size, 1–2000; defaults to 2000.

### Outputs

- `records` (`object[]`): Purchase order lines in this page. With the default fields each record has `id`, `bill_no`, `bill_date`, `document_status`, `supplier_number`, `material_number`, `material_name`, `qty`, `price`, `tax_price`, `tax_rate`, `amount`, and `all_amount`.
- `count` (`int`): Rows in this page.
- `has_more` (`bool`): True when the page was full and more rows may remain.

### Field reference (shared by field_keys, filter_string, and order_string)

Field keys come from the Kingdee BOS form metadata; all three inputs use the same identifiers. Two general rules:

- **Base-data fields** (supplier, material, organization, …) return the internal ID by themselves; append `.FNumber` for the code or `.FName` for the display name, e.g. `FSupplierId.FNumber`.
- **Querying entry-level fields flattens the result per entry line**: one record per order line, with header fields repeated on every line.

Common header fields:

| Field key | Meaning |
| --- | --- |
| `FID` | Order internal ID (unique; use as sort tiebreaker and incremental cursor) |
| `FBillNo` | Bill number |
| `FDate` | Purchase date |
| `FDocumentStatus` | Document status: `Z` draft, `A` created, `B` in approval, `C` approved, `D` re-approving |
| `FBillTypeID.FNumber` | Bill type code |
| `FBusinessType` | Business type |
| `FSupplierId.FNumber` / `FSupplierId.FName` | Supplier code / name |
| `FPurchaseOrgId.FNumber` / `FPurchaseOrgId.FName` | Purchase organization code / name |
| `FPurchaseDeptId.FNumber` / `FPurchaseDeptId.FName` | Purchase department code / name |
| `FPurchaserId.FNumber` / `FPurchaserId.FName` | Purchaser code / name |
| `FCloseStatus` | Close status: `A` open, `B` closed |
| `FCancelStatus` | Cancel status: `A` active, `B` cancelled |
| `FCreateDate` | Creation date |
| `FApproveDate` | Approval date (common incremental-sync filter) |

Common entry fields (`FPOOrderEntry` lines):

| Field key | Meaning |
| --- | --- |
| `FPOOrderEntry_FEntryId` | Entry internal ID (unique per line) |
| `FPOOrderEntry_FSeq` | Entry sequence number |
| `FMaterialId.FNumber` / `FMaterialId.FName` | Material code / name |
| `FMaterialName` | Material name (text) |
| `FUnitId.FNumber` | Purchase unit code |
| `FQty` | Quantity |
| `FPrice` | Unit price (excluding tax) |
| `FTaxPrice` | Unit price (including tax) |
| `FEntryTaxRate` | Tax rate (%) |
| `FAmount` | Amount (excluding tax) |
| `FAllAmount` | Amount (including tax) |
| `FDeliveryDate` | Delivery date |

These are the most used fields of the standard product; deployments may add custom fields (`F_xxx_` prefix). The authoritative list is your account book's form metadata: open the Purchase Order form in the BOS designer, or see the Kingdee community "WebAPI 接口说明书". An unknown field key makes Kingdee return a "field does not exist" error, which the node surfaces as-is.

### Example

Page through approved orders, newest first:

```js
function instances(info) {
  return { pull_orders: "kingdee.purchaseorder.list" };
}
function run(info) {
  let startRow = 0;
  let fetched = 0;
  while (true) {
    const page = info.instance("pull_orders").exec({
      filter_string: "FDocumentStatus='C'",
      order_string: "FDate DESC, FID DESC",
      start_row: startRow,
    });
    fetched += page.count;
    // process page.records here
    if (!page.has_more) break;
    startRow += page.count;
  }
  return { fetched };
}
```

Note: with descending order and offset paging, rows approved while you page shift later pages and cause duplicates. Fetching the "latest N" is fine; for a complete incremental sync prefer a cursor filter (e.g. `FID > last max ID` with the default `FID ASC`) or an `FApproveDate` window.
