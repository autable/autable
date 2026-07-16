## Kingdee purchase orders

Lists purchase order lines from Kingdee K3Cloud through the `ExecuteBillQuery` WebAPI (`PUR_PurchaseOrder` form). The node signs each request with the third-party app credentials and pages through results automatically until every matching row is fetched. Typically bound to a remote runner inside the network where the K3Cloud gateway is reachable.

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

- `filter_string` (`string`): Optional Kingdee filter expression, e.g. `FDocumentStatus='C'` for approved orders; empty fetches all rows.
- `field_keys` (`string[]`): Optional Kingdee field keys to fetch. When set, output records are keyed by the field keys themselves.
- `limit` (`int`): Optional page size per request, 1–2000; defaults to 2000.

### Outputs

- `records` (`object[]`): Purchase order lines. With the default fields each record has `id`, `bill_no`, `bill_date`, `document_status`, `supplier_number`, `material_number`, `material_name`, `qty`, `price`, `tax_price`, `tax_rate`, `amount`, and `all_amount`.
- `count` (`int`): Total rows fetched.

### Example

```js
function instances(info) {
  return { pull_orders: "kingdee.purchaseorder.list" };
}
function run(info) {
  const result = info.instance("pull_orders").exec({
    filter_string: "FDocumentStatus='C' AND FDate>='2026-01-01'"
  });
  return { fetched: result.count };
}
```
