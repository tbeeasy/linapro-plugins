# linapro-moka-report-sync 配置说明

## 报表映射（reportMappings）

通过 sys_config 键 `plugin.linapro-moka-report-sync.reportMappings` 配置一个 JSON 数组，
每项把一个 Moka 报表绑定到一张飞书多维表格。

### 扁平同步（shape: flat，缺省）

报表一行对应 Bitable 一行，按 `uniqueFields` 唯一键做新增/重写/冻结。

```json
[
  {
    "reportId": 100001,
    "appToken": "bascnXXXXXXXXXXXX",
    "tableId": "tblXXXXXXXXXXXX",
    "uniqueFields": ["工号"],
    "keySeparator": "-",
    "remark": "HCM 员工信息同步",
    "enable": true,
    "source": "hcm"
  }
]
```

### 行过滤（skipIf，仅 flat 形态）

在扁平映射上声明 `skipIf` 数组，可把不满足条件的报表行在写入前丢弃。典型场景：招聘月度报表
返回了指定日期之后的全部月份，而运营只想同步最近一段时间的数据，历史月份行不必写入。

每条规则含四个字段：

| 字段 | 说明 |
|---|---|
| `kind` | 比较类型，从封闭注册表解析。当前仅支持 `"before"`（源列时间**早于**阈值则跳过该行） |
| `source` | 源列名，取该列单元格值参与比较。可为原始报表列，也可为 `derivedColumns` 派生出的列 |
| `format` | 源列值与阈值的解析格式，用 GoFrame `gtime` 方言：`Y`=年、`m`=月、`d`=日（如 `"Y-m"`、`"Y-m-d"`）。**不要**写 Go 参考时间布局（`2006-01`） |
| `value` | 阈值字面量，按 `format` 解析后与源列值比较。是**固定值**，不支持相对当前时间的偏移（如 `now-3M`） |

**语义要点：**

- **比较方式**：源列值与阈值都按 `format` 解析为东八区时间戳后比较，不是字符串比大小；同一份配置在不同部署时区下产出相同判定。
- **`before` 判定**：源列时间**严格早于**阈值时跳过；等于或晚于阈值保留。
- **解析失败保留行**：源列值为空、或源列值与阈值任一无法按 `format` 解析时，该规则对该行判定为「无法判定」，该行**保留**（不跳过）并记 warn。宁可多同步一行（有幂等冻结兜底），也不因格式变更静默丢数据。
- **多条规则取并集**：任一规则命中即丢弃该行。
- **不生效可从日志定位**：`format` 与源数据形状不匹配、阈值格式非法、`source` 列名写错时，规则对相关行「无法判定」，日志会记 warn 标明规则标识（`kind`/`source`/`format`/`value`）与被保留的行数；同步完成日志追加「过滤=N」表示被丢弃的行数。
- **pivot 不支持**：转置形态（`shape: pivot`）不接入 `skipIf`；配置了也会被忽略并记 warn，同步照常执行。

**与 `derivedColumns` 组合：**

下例用 `derivedColumns` 从原始日期列 `申请时间`（如 `2026-03`）派生出 `招聘年份`（`2026`）与
`招聘月份`（`3月`）两列，供 Bitable 展示或作为 `uniqueFields` 建键；再用 `skipIf` 按日期列
`申请时间` 过滤，跳过 `2026-01` 之前的行。`date` 派生的 `targets` 里，键是目标列名、值是 Go 时间
布局（`2006`=四位年、`1月`=不补零月份加「月」字）。

```json
[
  {
    "reportId": 100003,
    "appToken": "bascnZZZZZZZZZZZZ",
    "tableId": "tblZZZZZZZZZZZZ",
    "uniqueFields": ["工号", "招聘年份", "招聘月份"],
    "keySeparator": "-",
    "remark": "招聘月度报表（仅同步 2026-01 及以后）",
    "enable": true,
    "source": "recruit",
    "derivedColumns": [
      {
        "kind": "date",
        "sources": ["申请时间"],
        "targets": { "招聘年份": "2006", "招聘月份": "1月" }
      }
    ],
    "skipIf": [
      { "kind": "before", "source": "申请时间", "format": "Y-m", "value": "2026-01" }
    ]
  }
]
```

> `skipIf` 的 `source` 需指向**日期形状**的列。上例直接用原始列 `申请时间`（`Y-m`）；派生列 `招聘年份`
> （`2006`）、`招聘月份`（`1月`）是展示/建键用的文本，不适合作 `before` 比较。若要按派生列过滤，应先
> 派生出一个日期形状（如 `Y-m`）的列再引用它。

### 转置同步（shape: pivot）

适用于「Moka 报表行列与目标 Bitable 行列方向相反」的场景。
典型案例：招聘漏斗图报表中月份为行、指标为列，而目标表要求指标为行、月份为列。

**源列名与目标索引列名解耦**：`pivotHeaderColumn` 是**源报表**中「其行值将成为目标列头」的列名
（如日期列 `公共日期`，行值 `2026-01`/`2026-02`/…）；`pivotIndexColumn` 是**转置后承载指标名**的
输出索引列名，须与目标 Bitable 的指标标签列同名（如 `招聘漏斗图`）。二者常不同名，需分别声明；
`pivotIndexColumn` 不会回退到 `pivotHeaderColumn`，未声明则本轮跳过。

**运营前置条件**：目标 Bitable 表需由运营预先建好各月份列（如 `2026-01`、`2026-02`、…）；
插件只填写已存在的月份列的值，不会自动创建列。`年度` 等由 Bitable 公式字段自算的汇总列
插件不写入、不触碰。

```json
[
  {
    "reportId": 100002,
    "appToken": "bascnYYYYYYYYYYYY",
    "tableId": "tblYYYYYYYYYYYY",
    "remark": "招聘漏斗图转置同步",
    "enable": true,
    "source": "recruit",

    "shape": "pivot",
    "pivotHeaderColumn": "公共日期",
    "pivotIndexColumn": "招聘漏斗图",

    "uniqueFields": ["招聘漏斗图"],
    "keySeparator": "-"
  }
]
```

**字段说明：**

| 字段 | 说明 |
|---|---|
| `shape` | `"flat"`（缺省，扁平 upsert）或 `"pivot"`（转置） |
| `pivotHeaderColumn` | pivot 形态必填：**源报表**中「行值将成为目标列头」的列名（如日期列 `"公共日期"`，其行值为 `"2026-01"`/`"2026-02"`/…） |
| `pivotIndexColumn` | pivot 形态必填：**转置后承载指标名**的输出索引列名，须与目标表的指标标签列同名（如 `"招聘漏斗图"`）；不回退到 `pivotHeaderColumn` |
| `uniqueFields` | pivot 形态下填 `[pivotIndexColumn]`，以指标名为唯一键做幂等 upsert |
| `keySeparator` | 复合键分隔符，单列键可保留默认 `"-"` |

**pivot 形态的约束：**
- `derivedColumns` 与 `personFieldSources` 在 pivot 形态下不生效；若配置则忽略并记录 warn。
- `skipIf` 行过滤在 pivot 形态下不生效；若配置则忽略并记录 warn。
- `pivotHeaderColumn` 或 `pivotIndexColumn` 未配置、或源报表中不存在 `pivotHeaderColumn` 列时，本轮该映射被跳过（记 warn），其他映射照常执行。
- 目标 Bitable 表中不存在的月份列值会被忽略（记 warn），不阻断同步。
