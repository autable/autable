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

- `filter_string` (`string`)：金蝶过滤表达式（SQL WHERE 语法），例如 `FDocumentStatus='C' AND FDate>='2026-01-01'`；留空匹配全部。可引用下方任意字段标识。
- `field_keys` (`string[]`)：自定义查询字段，见下方字段参考。设置后输出记录以字段标识本身为键；不设置时使用内置的 13 个默认列。
- `order_string` (`string`)：排序表达式（SQL ORDER BY 语法），例如 `FDate DESC, FID DESC` 从最新开始；默认 `FID ASC`。分页时排序键必须稳定唯一（建议总是以 `FID` 兜底），否则页间会重复或漏行。
- `start_row` (`int`)：本页起始行偏移（从 0 开始），默认 0。
- `limit` (`int`)：单页行数，1–2000，默认 2000。

### 输出

- `records` (`object[]`)：本页采购订单明细。默认字段下每条记录包含 `id`、`bill_no`、`bill_date`、`document_status`、`supplier_number`、`material_number`、`material_name`、`qty`、`price`、`tax_price`、`tax_rate`、`amount`、`all_amount`。
- `count` (`int`)：本页行数。
- `has_more` (`bool`)：本页填满时为 `true`，表示可能还有后续数据。

### 字段参考（field_keys / filter_string / order_string 通用）

字段标识来自金蝶 BOS 表单元数据，三个输入共用同一套标识。两条通用规则：

- **基础资料字段**（供应商、物料、组织等）本身返回内码整数，加 `.FNumber` 取编码、`.FName` 取名称，例如 `FSupplierId.FNumber`。
- **查询含明细字段时按分录行展开**：每个分录行一条记录，表头字段在每行重复。

常用表头字段：

| 字段标识 | 含义 |
| --- | --- |
| `FID` | 订单内码（唯一，适合做排序兜底和增量游标） |
| `FBillNo` | 单据编号 |
| `FDate` | 采购日期 |
| `FDocumentStatus` | 单据状态：`Z` 暂存、`A` 创建、`B` 审核中、`C` 已审核、`D` 重新审核 |
| `FBillTypeID.FNumber` | 单据类型编码 |
| `FBusinessType` | 业务类型 |
| `FSupplierId.FNumber` / `FSupplierId.FName` | 供应商编码 / 名称 |
| `FPurchaseOrgId.FNumber` / `FPurchaseOrgId.FName` | 采购组织编码 / 名称 |
| `FPurchaseDeptId.FNumber` / `FPurchaseDeptId.FName` | 采购部门编码 / 名称 |
| `FPurchaserId.FNumber` / `FPurchaserId.FName` | 采购员编码 / 名称 |
| `FCloseStatus` | 关闭状态：`A` 未关闭、`B` 已关闭 |
| `FCancelStatus` | 作废状态：`A` 未作废、`B` 已作废 |
| `FCreateDate` | 创建日期 |
| `FApproveDate` | 审核日期（增量同步常用过滤字段） |

常用明细字段（`FPOOrderEntry` 分录）：

| 字段标识 | 含义 |
| --- | --- |
| `FPOOrderEntry_FEntryId` | 分录内码（明细行唯一标识） |
| `FPOOrderEntry_FSeq` | 分录行号 |
| `FMaterialId.FNumber` / `FMaterialId.FName` | 物料编码 / 名称 |
| `FMaterialName` | 物料名称（文本） |
| `FUnitId.FNumber` | 采购单位编码 |
| `FQty` | 采购数量 |
| `FPrice` | 单价（不含税） |
| `FTaxPrice` | 含税单价 |
| `FEntryTaxRate` | 税率（%） |
| `FAmount` | 金额（不含税） |
| `FAllAmount` | 价税合计 |
| `FDeliveryDate` | 交货日期 |

以上是标准产品里最常用的字段；各账套可能有二次开发字段（`F_xxx_` 前缀）。完整清单以自己账套的表单元数据为准：在金蝶客户端用 BOS 设计器打开「采购订单」查看字段标识，或参考金蝶社区的《WebAPI 接口说明书》。字段写错时金蝶会返回“字段不存在”类错误，节点会原样透出。

### 示例

从最新的已审核订单开始，分页取数：

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
    // 在这里处理 page.records
    if (!page.has_more) break;
    startRow += page.count;
  }
  return { fetched };
}
```

注意：倒序 + 偏移分页期间若有新单审核通过，后续页会整体后移导致重复行。只取「最近 N 条」没有影响；做完整增量同步建议改用游标过滤（如 `FID > 上次最大内码` 配合默认 `FID ASC`），或按 `FApproveDate` 日期窗口过滤。
