## 金蝶采购订单

通过金蝶云星空 `ExecuteBillQuery` WebAPI（`PUR_PurchaseOrder` 表单）查询采购订单明细。节点使用第三方应用凭据对请求签名，每次调用只返回一页数据：调用方通过 `start_row` 和 `limit` 控制分页，用 `has_more` 判断是否继续。通常绑定到能访问 K3Cloud 网关的内网远程运行器上执行。

### 密钥

- `app_secret` (`string`)：第三方应用密钥。

### 变量

- `server_url` (`string`)：K3Cloud 网关地址，例如 `https://erp.example.com/K3Cloud`。
- `acct_id` (`string`)：账套 ID。
- `user_name` (`string`)：集成用户名。
- `app_id` (`string`)：第三方应用 ID（金蝶开发者中心里 `xxxxxx_yyyy` 形式的值）。
- `org_num` (`string`)：多组织环境的组织编码，单组织留空。
- `skip_tls_verify` (`string`)：设为 `true` 时接受自签名网关证书，默认校验 TLS。

### 输入

- `filter_string` (`string`)：金蝶过滤表达式，例如 `FDocumentStatus='C'`（已审核）；留空匹配全部。
- `field_keys` (`string[]`)：自定义查询字段。设置后输出记录以字段名本身为键。
- `start_row` (`int`)：本页起始行偏移（从 0 开始），默认 0。
- `limit` (`int`)：单页行数，1–2000，默认 2000。

### 输出

- `records` (`object[]`)：本页采购订单明细。默认字段下每条记录包含 `id`、`bill_no`、`bill_date`、`document_status`、`supplier_number`、`material_number`、`material_name`、`qty`、`price`、`tax_price`、`tax_rate`、`amount`、`all_amount`。
- `count` (`int`)：本页行数。
- `has_more` (`bool`)：本页填满时为 `true`，表示可能还有后续数据。

### 示例

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
    // 在这里处理 page.records
    if (!page.has_more) break;
    startRow += page.count;
  }
  return { fetched };
}
```
