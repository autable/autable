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

- `filter_string` (`string`): Optional Kingdee filter expression, e.g. `FDocumentStatus='C'` for approved orders; empty matches all rows.
- `field_keys` (`string[]`): Optional Kingdee field keys to fetch. When set, output records are keyed by the field keys themselves.
- `start_row` (`int`): Optional zero-based row offset to start the page at; defaults to 0.
- `limit` (`int`): Optional page size, 1–2000; defaults to 2000.

### Outputs

- `records` (`object[]`): Purchase order lines in this page. With the default fields each record has `id`, `bill_no`, `bill_date`, `document_status`, `supplier_number`, `material_number`, `material_name`, `qty`, `price`, `tax_price`, `tax_rate`, `amount`, and `all_amount`.
- `count` (`int`): Rows in this page.
- `has_more` (`bool`): True when the page was full and more rows may remain.

### Example

```js
function instances(info) {
  return { pull_orders: "kingdee.purchaseorder.list" };
}
function run(info) {
  let startRow = 0;
  let fetched = 0;
  while (true) {
    const page = info.instance("pull_orders").exec({
      filter_string: "FDocumentStatus='C' AND FDate>='2026-01-01'",
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
