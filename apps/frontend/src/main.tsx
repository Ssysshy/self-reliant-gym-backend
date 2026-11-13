import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Button,
  ConfigProvider,
  Divider,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  message,
} from "antd";
import zhCN from "antd/locale/zh_CN";
import "./style.less";

interface Item {
  Name: string;
  Sets: number;
  Reps: number;
  Rest: number;
}
interface Day {
  Weekday: number;
  Focus: string;
  Items: Item[];
}
interface Plan {
  ID: number;
  Revision: number;
  Name: string;
  Published: boolean;
  Days: Day[];
  // 小程序里的展示信息：封面、难度、单次时长、详情页分段文案。老数据可能没有
  Meta: PlanMeta | null;
}
interface Block {
  Title: string;
  Text?: string;
  Lines?: string[];
}
interface PlanMeta {
  Cover: string;
  Level: string;
  Minutes: number;
  Kcal: number;
  Blocks: Block[];
}
interface Product {
  Days: number;
  Price: number;
  Enabled: boolean;
  // 套餐卡上的说明，留空时小程序显示「会员有效期 N 天」
  Note: string;
  // XpayID 是后台填的商品 ID（可空）；Xpay 是实际生效值（空则回落环境变量）；
  // Ready 表示支付配置齐全、现在真的能下单
  XpayID: string;
  Xpay: string;
  Ready: boolean;
}
interface FeedbackRow {
  ID: number;
  Owner: number;
  OwnerCode: string;
  Content: string;
  Status: string;
  HandledBy: string | null;
  Note: string | null;
  HandledAt: string | null;
  CreatedAt: string;
}
interface Audit {
  ID: number;
  Actor: string;
  Action: string;
  Target: number;
  CreatedAt: string;
}
interface Grant {
  ID: number;
  Days: number;
  Operator: string;
  Reason: string;
  Revoked: string | null;
}
interface Order {
  ID: string;
  Owner: number;
  Price: number;
  Status: string;
  Days: number;
}
interface Member {
  ID: number;
  Code: string;
  Experience: string;
  Expires: string;
}
interface Exercise {
  ID: number;
  Name: string;
  Muscle: string;
  SubMuscle: string;
  Category: string;
  Equipment: string;
  Guide: string;
  Setup: string;
  AuxMuscles: string;
  Mistakes: string;
  ImageURL: string;
}
interface Part {
  Name: string;
  Subs: string[] | null;
}
interface Meta {
  Parts: Part[];
  Categories: string[];
}
const emptyExercise = (): Exercise => ({
  ID: 0,
  Name: "",
  Muscle: "",
  SubMuscle: "",
  Category: "",
  Equipment: "",
  Guide: "",
  Setup: "",
  AuxMuscles: "",
  Mistakes: "",
  ImageURL: "",
});
async function api<T>(
  path: string,
  body?: unknown,
  method?: string,
): Promise<T> {
  const r = await fetch("/api/admin/v1" + path, {
    method: method || (body ? "POST" : "GET"),
    headers: { "Content-Type": "application/json", "X-Gym-Admin": "1" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const value = await r.json();
  if (!r.ok) {
    if (r.status === 401) window.dispatchEvent(new Event("expired"));
    throw new Error(value.error || "请求失败");
  }
  return value as T;
}
const report = (err: unknown): void => {
  void message.error(err instanceof Error ? err.message : "操作失败");
};
const blankDay = (): Day => ({
  Weekday: 1,
  Focus: "全身",
  Items: [{ Name: "坐姿推胸", Sets: 3, Reps: 10, Rest: 60 }],
});
// 新建模板时的展示信息默认值：封面留空就由小程序显示深色渐变块
const blankMeta = (): PlanMeta => ({
  Cover: "",
  Level: "入门",
  Minutes: 0,
  Kcal: 0,
  Blocks: [],
});
// 会员档位表单：新增与编辑共用。售价在表单里按「元」输入，提交时换回分（后端统一存分）。
function ProductForm({
  entry,
  onDone,
}: {
  entry: Product | "new";
  onDone: () => void;
}): React.JSX.Element {
  const isNew = entry === "new";
  return (
    <Form
      key={isNew ? "new" : entry.Days}
      layout="vertical"
      initialValues={
        isNew
          ? { Days: 90, Price: 39.9, Note: "", XpayID: "", Enabled: true }
          : { ...entry, Price: entry.Price / 100 }
      }
      onFinish={async (v: {
        Days: number;
        Price: number;
        Note?: string;
        XpayID?: string;
        Enabled?: boolean;
      }) => {
        try {
          const body = {
            Days: Number(v.Days),
            Price: Math.round(Number(v.Price) * 100),
            Note: v.Note || "",
            XpayID: (v.XpayID || "").trim(),
            Enabled: !!v.Enabled,
          };
          if (isNew) await api("/products", body);
          else await api("/products/" + entry.Days, body, "PUT");
          onDone();
        } catch (e) {
          report(e);
        }
      }}
    >
      <Space size="large" align="start">
        <Form.Item
          name="Days"
          label="有效天数"
          rules={[{ required: true }]}
          extra="天数即档位身份，不能和已有档位重复"
        >
          <InputNumber min={1} max={3650} addonAfter="天" />
        </Form.Item>
        <Form.Item
          name="Price"
          label="售价（元）"
          rules={[{ required: true }]}
        >
          <InputNumber
            min={0}
            max={100000}
            precision={2}
            addonBefore="¥"
            style={{ width: 140 }}
          />
        </Form.Item>
        <Form.Item name="Enabled" label="上架" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Space>
      <Form.Item
        name="Note"
        label="套餐说明"
        extra="显示在套餐卡天数下方，最多 32 个字；留空则显示「会员有效期 N 天」"
      >
        <Input maxLength={32} showCount placeholder="例：覆盖一个完整训练周期" />
      </Form.Item>
      <Form.Item
        name="XpayID"
        label="微信虚拟支付商品 ID"
        extra="在微信「虚拟支付」后台建好商品后把 productId 填这里；留空则用环境变量 XPAY_PRODUCT_<天数>（存量三档就是这种方式）"
      >
        <Input allowClear placeholder="留空用环境变量" />
      </Form.Item>
      <Button type="primary" htmlType="submit">
        {isNew ? "新增" : "保存"}
      </Button>
    </Form>
  );
}

// 动作资料表单：细分肌群的可选值依赖已选部位，所以单独拆一个组件拿 Form 实例。
function ExerciseForm({
  entry,
  meta,
  onDone,
}: {
  entry: Exercise;
  meta: Meta | null;
  onDone: () => void;
}): React.JSX.Element {
  const [form] = Form.useForm();
  const part = Form.useWatch("Muscle", form) as string | undefined;
  const subs = meta?.Parts.find((p) => p.Name === part)?.Subs || [];
  const previous = useRef(part);
  // 换部位时清掉上一部位留下的细分，首次渲染（编辑已有动作）不动它。
  useEffect(() => {
    if (previous.current !== part) {
      form.setFieldValue("SubMuscle", undefined);
      previous.current = part;
    }
  }, [part, form]);
  return (
    <Form
      form={form}
      initialValues={entry}
      layout="vertical"
      onFinish={async (v: Exercise) => {
        try {
          await api("/exercises", { ...entry, ...v });
          onDone();
        } catch (e) {
          report(e);
        }
      }}
    >
      <Form.Item name="Name" label="动作名称" rules={[{ required: true }]}>
        <Input />
      </Form.Item>
      <Space align="start">
        <Form.Item
          name="Muscle"
          label="部位"
          rules={[{ required: true, message: "请选择部位" }]}
        >
          <Select
            style={{ width: 130 }}
            placeholder="选择部位"
            options={(meta?.Parts || []).map((p) => ({
              label: p.Name,
              value: p.Name,
            }))}
          />
        </Form.Item>
        <Form.Item name="SubMuscle" label="细分肌群">
          <Select
            style={{ width: 130 }}
            allowClear
            disabled={subs.length === 0}
            placeholder={subs.length ? "可留空" : "该部位无细分"}
            options={subs.map((s) => ({ label: s, value: s }))}
          />
        </Form.Item>
        <Form.Item
          name="Category"
          label="器材分类"
          rules={[{ required: true, message: "请选择器材分类" }]}
        >
          <Select
            style={{ width: 130 }}
            placeholder="选择分类"
            options={(meta?.Categories || []).map((c) => ({
              label: c,
              value: c,
            }))}
          />
        </Form.Item>
        <Form.Item
          name="Equipment"
          label="具体器材"
          rules={[{ required: true }]}
        >
          <Input style={{ width: 160 }} placeholder="推胸机 / 训练垫" />
        </Form.Item>
      </Space>
      <Form.Item name="Setup" label="器材调节" rules={[{ required: true }]}>
        <Input />
      </Form.Item>
      <Form.Item name="Guide" label="动作步骤" rules={[{ required: true }]}>
        <Input.TextArea rows={3} />
      </Form.Item>
      <Form.Item name="AuxMuscles" label="辅助肌群">
        <Input />
      </Form.Item>
      <Form.Item name="Mistakes" label="常见错误">
        <Input.TextArea rows={2} />
      </Form.Item>
      <Form.Item
        name="ImageURL"
        label="示范图 HTTPS 地址"
        rules={[
          { pattern: /^https:\/\/\S+$/, message: "示范图须使用 HTTPS 地址" },
        ]}
      >
        <Input placeholder="https://" />
      </Form.Item>
      <Button htmlType="submit" type="primary">
        保存资料
      </Button>
    </Form>
  );
}
function Console(): React.JSX.Element {
  const [logged, setLogged] = useState(false),
    [page, setPage] = useState("训练模板"),
    [plans, setPlans] = useState<Plan[]>([]),
    [members, setMembers] = useState<Member[]>([]),
    [edit, setEdit] = useState<Plan | null>(null),
    [member, setMember] = useState<Member | null>(null),
    // 注销账号：purge 是要注销的人，purgeCode 是照着编号再输一遍的确认值
    [purge, setPurge] = useState<Member | null>(null),
    [purgeCode, setPurgeCode] = useState(""),
    [intent, setIntent] = useState("draft"),
    [orderRows, setOrderRows] = useState<Order[]>([]),
    [grants, setGrants] = useState<Grant[]>([]),
    [grantUser, setGrantUser] = useState(0),
    [revokeId, setRevokeId] = useState(0),
    [reason, setReason] = useState(""),
    [products, setProducts] = useState<Product[]>([]),
    // 正在编辑的档位："new" 是新增，Product 是编辑，null 表示弹窗关闭
    [product, setProduct] = useState<Product | "new" | null>(null),
    [admin, setAdmin] = useState(false),
    [audit, setAudit] = useState<Audit[]>([]),
    [catalog, setCatalog] = useState<Exercise[]>([]),
    [feedbacks, setFeedbacks] = useState<FeedbackRow[]>([]),
    [fbStatus, setFbStatus] = useState("待处理"),
    [fbEdit, setFbEdit] = useState<FeedbackRow | null>(null),
    [fbNote, setFbNote] = useState(""),
    [fbOwner, setFbOwner] = useState(""),
    [entry, setEntry] = useState<Exercise | null>(null),
    [meta, setMeta] = useState<Meta | null>(null),
    [part, setPart] = useState(""),
    [gear, setGear] = useState(""),
    [kw, setKw] = useState(""),
    [query, setQuery] = useState("");
  async function refresh(): Promise<void> {
    const rows = await api<Plan[]>("/templates");
    setPlans(rows);
    setCatalog(await api<Exercise[]>("/exercises"));
    setMeta(await api<Meta>("/categories"));
    setAdmin((await api<{ admin: boolean }>("/me")).admin);
    setLogged(true);
  }
  useEffect(() => {
    const expire = (): void => setLogged(false);
    window.addEventListener("expired", expire);
    void refresh().catch(() => {});
    return (): void => window.removeEventListener("expired", expire);
  }, []);
  async function loadMembers(): Promise<void> {
    setMembers(await api<Member[]>("/members?id=" + encodeURIComponent(query)));
  }
  async function loadFeedbacks(status: string): Promise<void> {
    setFbStatus(status);
    const qs = [
      status ? "status=" + encodeURIComponent(status) : "",
      fbOwner.trim() ? "owner=" + encodeURIComponent(fbOwner.trim()) : "",
    ].filter(Boolean);
    setFeedbacks(
      await api<FeedbackRow[]>(
        "/feedbacks" + (qs.length ? "?" + qs.join("&") : ""),
      ),
    );
  }
  async function openPage(p: string): Promise<void> {
    if (p === "操作记录") setAudit(await api<Audit[]>("/audit"));
    if (p === "会员套餐") setProducts(await api<Product[]>("/products"));
    if (p === "会员订单") setOrderRows(await api<Order[]>("/orders"));
    if (p === "会员管理") await loadMembers();
    if (p === "用户反馈") await loadFeedbacks(fbStatus);
    if (p === "训练资料" || p === "训练模板")
      setCatalog(await api<Exercise[]>("/exercises"));
    setPage(p);
  }
  async function submitPlan(values: Plan): Promise<void> {
    try {
      // 表单里的要点是一段换行分隔的文本（Form.Item 的 normalize 已经拆回数组），
      // 这里只做清理：去掉空段、没标题的段，并把空的 Text/Lines 丢掉，别把空字段塞进 JSON
      const meta = values.Meta;
      const Blocks = (meta?.Blocks || [])
        .map((b) => ({
          Title: (b.Title || "").trim(),
          Text: (b.Text || "").trim(),
          Lines: (b.Lines || []).filter((s) => (s || "").trim()),
        }))
        .filter((b) => b.Title && (b.Text || b.Lines.length))
        .map((b) =>
          b.Lines.length
            ? { Title: b.Title, Lines: b.Lines }
            : { Title: b.Title, Text: b.Text },
        );
      await api("/templates", {
        ...edit,
        ...values,
        Meta: {
          Cover: (meta?.Cover || "").trim(),
          Level: meta?.Level || "",
          Minutes: Number(meta?.Minutes) || 0,
          Kcal: Number(meta?.Kcal) || 0,
          Blocks,
        },
        Intent: intent,
      });
      await refresh();
      setEdit(null);
    } catch (e) {
      report(e);
    }
  }
  // 删除模板：用户按模板建的计划是独立副本，删模板不影响任何人的训练安排，
  // 但会立刻从小程序的推荐列表里消失，所以先弹一次确认
  function removePlan(p: Plan): void {
    Modal.confirm({
      title: "删除模板「" + p.Name + "」？",
      content:
        "删除后它会从后台和小程序的推荐计划里一起消失。已经按它创建的用户计划是独立的一份，不受影响。",
      okText: "删除",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: async () => {
        try {
          await api(`/templates/${p.ID}`, undefined, "DELETE");
          await refresh();
        } catch (e) {
          report(e);
        }
      },
    });
  }
  if (!logged)
    return (
      <main className="login">
        <small>撸铁不求人</small>
        <h1>运营后台</h1>
        <Form
          layout="vertical"
          onFinish={async (v: { Account: string; Password: string }) => {
            try {
              await api("/login", v);
              await refresh();
            } catch (e) {
              report(e);
            }
          }}
        >
          <Form.Item
            label="运营账号"
            name="Account"
            rules={[{ required: true }]}
          >
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item label="密码" name="Password" rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Button block type="primary" htmlType="submit">
            登录
          </Button>
        </Form>
      </main>
    );
  return (
    <div className="layout">
      <aside>
        <strong>撸铁不求人</strong>
        <small>运营后台</small>
        {[
          "训练模板",
          "训练资料",
          "会员管理",
          "会员套餐",
          "会员订单",
          "用户反馈",
          ...(admin ? ["操作记录"] : []),
        ].map((p) => (
          <button
            key={p}
            aria-current={page === p}
            onClick={() => void openPage(p).catch(report)}
          >
            {p}
          </button>
        ))}
        <button
          onClick={() =>
            void api("/logout", {})
              .then(() => setLogged(false))
              .catch(report)
          }
        >
          退出登录
        </button>
      </aside>
      <main>
        <h1>{page}</h1>
        {page === "训练模板" && (
          <>
            <Button
              type="primary"
              onClick={() =>
                setEdit({
                  ID: 0,
                  Revision: 0,
                  Name: "",
                  Published: false,
                  Days: [blankDay()],
                  Meta: blankMeta(),
                })
              }
            >
              新建周模板
            </Button>
            <Table
              rowKey="ID"
              dataSource={plans}
              columns={[
                {
                  title: "封面",
                  width: 96,
                  render: (_, p: Plan) =>
                    p.Meta?.Cover ? (
                      <img className="cover-thumb" src={p.Meta.Cover} alt="" />
                    ) : (
                      <span className="dim">未设置</span>
                    ),
                },
                { title: "模板名称", dataIndex: "Name" },
                {
                  title: "每周训练",
                  render: (_, p: Plan) => p.Days.length + " 天",
                },
                {
                  title: "状态",
                  render: (_, p: Plan) => (p.Published ? "已发布" : "草稿"),
                },
                {
                  title: "",
                  render: (_, p: Plan) => (
                    <Space>
                      <Button onClick={() => setEdit(p)}>配置</Button>
                      <Button
                        danger
                        type="text"
                        onClick={() => removePlan(p)}
                      >
                        删除
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
          </>
        )}
        {page === "训练资料" && (
          <>
            <Space style={{ marginBottom: 16 }} wrap>
              <Select
                style={{ width: 120 }}
                placeholder="全部部位"
                allowClear
                value={part || undefined}
                onChange={(v: string | undefined) => setPart(v || "")}
                options={(meta?.Parts || []).map((p) => ({
                  label: p.Name,
                  value: p.Name,
                }))}
              />
              <Select
                style={{ width: 130 }}
                placeholder="全部器材分类"
                allowClear
                value={gear || undefined}
                onChange={(v: string | undefined) => setGear(v || "")}
                options={(meta?.Categories || []).map((c) => ({
                  label: c,
                  value: c,
                }))}
              />
              <Input
                style={{ width: 200 }}
                placeholder="搜索动作或器材"
                allowClear
                value={kw}
                onChange={(e) => setKw(e.target.value)}
              />
              <Button onClick={() => setEntry(emptyExercise())}>新增动作</Button>
            </Space>
            <Table
              rowKey="ID"
              dataSource={catalog.filter(
                (x) =>
                  (!part || x.Muscle === part) &&
                  (!gear || x.Category === gear) &&
                  (!kw ||
                    (x.Name + x.SubMuscle + x.Equipment).includes(kw.trim())),
              )}
              locale={{ emptyText: "没有符合条件的动作" }}
              pagination={{ pageSize: 20, hideOnSinglePage: true }}
              columns={[
                {
                  title: "动作",
                  dataIndex: "Name",
                  render: (v: string, x: Exercise) => (
                    <Space>
                      {x.ImageURL && (
                        <img
                          src={x.ImageURL}
                          alt=""
                          style={{ width: 40, height: 40, objectFit: "contain" }}
                        />
                      )}
                      <span>{v}</span>
                    </Space>
                  ),
                },
                {
                  title: "部位",
                  dataIndex: "Muscle",
                  width: 90,
                  render: (v: string) => <Tag color="blue">{v || "-"}</Tag>,
                },
                {
                  title: "细分肌群",
                  dataIndex: "SubMuscle",
                  width: 100,
                  render: (v: string) => v || "-",
                },
                {
                  title: "器材分类",
                  dataIndex: "Category",
                  width: 100,
                  render: (v: string) => v || "-",
                },
                { title: "具体器材", dataIndex: "Equipment", width: 150 },
                {
                  title: "",
                  width: 80,
                  render: (_, x: Exercise) => (
                    <Button onClick={() => setEntry(x)}>编辑</Button>
                  ),
                },
              ]}
            />
          </>
        )}
        {page === "会员套餐" && (
          <>
            <Space style={{ marginBottom: 16 }} wrap>
              <Button
                type="primary"
                disabled={!admin}
                onClick={() => setProduct("new")}
              >
                新增套餐
              </Button>
              <span style={{ color: "#999" }}>
                小程序只展示「已上架」的档位；商品 ID 没配好的档位会显示「支付未就绪」，用户那边是「暂未开放」。
              </span>
            </Space>
            <Table
              rowKey="Days"
              dataSource={products}
              locale={{ emptyText: "还没有档位，点「新增套餐」建一个" }}
              columns={[
                {
                  title: "有效天数",
                  dataIndex: "Days",
                  width: 110,
                  render: (v: number) => v + " 天",
                },
                {
                  title: "售价（元）",
                  width: 110,
                  render: (_: unknown, p: Product) => p.Price / 100,
                },
                {
                  title: "套餐说明",
                  dataIndex: "Note",
                  render: (v: string) => v || "-",
                },
                {
                  title: "微信商品 ID",
                  width: 240,
                  render: (_: unknown, p: Product) =>
                    p.Xpay ? (
                      <Space size={4}>
                        <span>{p.Xpay}</span>
                        {!p.XpayID && <Tag>环境变量</Tag>}
                      </Space>
                    ) : (
                      <Tag color="red">未配置</Tag>
                    ),
                },
                {
                  title: "状态",
                  width: 190,
                  render: (_: unknown, p: Product) => (
                    <Space size={4}>
                      <Tag color={p.Enabled ? "green" : "default"}>
                        {p.Enabled ? "已上架" : "已下架"}
                      </Tag>
                      {p.Enabled && !p.Ready && (
                        <Tag color="orange">支付未就绪</Tag>
                      )}
                    </Space>
                  ),
                },
                {
                  title: "",
                  width: 160,
                  render: (_: unknown, p: Product) => (
                    <Space size={4}>
                      <Button disabled={!admin} onClick={() => setProduct(p)}>
                        编辑
                      </Button>
                      <Button
                        danger
                        disabled={!admin}
                        onClick={() =>
                          Modal.confirm({
                            title: "删除 " + p.Days + " 天套餐？",
                            content:
                              "删掉后小程序不再展示这一档，已付订单与已到账的会员不受影响。",
                            okText: "删除",
                            okButtonProps: { danger: true },
                            onOk: async () => {
                              try {
                                await api(
                                  "/products/" + p.Days,
                                  undefined,
                                  "DELETE",
                                );
                                await openPage("会员套餐");
                              } catch (e) {
                                report(e);
                              }
                            },
                          })
                        }
                      >
                        删除
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
            <Modal
              open={!!product}
              title={product === "new" ? "新增套餐" : "编辑套餐"}
              footer={null}
              destroyOnClose
              onCancel={() => setProduct(null)}
            >
              {product && (
                <ProductForm
                  entry={product}
                  onDone={async () => {
                    setProduct(null);
                    await openPage("会员套餐");
                  }}
                />
              )}
            </Modal>
          </>
        )}
        {page === "操作记录" && (
          <Table
            rowKey="ID"
            dataSource={audit}
            columns={[
              { title: "操作人", dataIndex: "Actor" },
              { title: "操作", dataIndex: "Action" },
              { title: "对象编号", dataIndex: "Target" },
              { title: "时间", dataIndex: "CreatedAt" },
            ]}
          />
        )}
        {page === "用户反馈" && (
          <>
            <Space style={{ marginBottom: 16 }} wrap>
              <Input.Search
                placeholder="按用户编号查（如 U3M8Q2K）"
                allowClear
                style={{ width: 260 }}
                value={fbOwner}
                onChange={(e) => setFbOwner(e.target.value)}
                onSearch={() => void loadFeedbacks(fbStatus).catch(report)}
              />
              {["待处理", "已处理", ""].map((s) => (
                <Button
                  key={s || "全部"}
                  type={fbStatus === s ? "primary" : "default"}
                  onClick={() => void loadFeedbacks(s).catch(report)}
                >
                  {s || "全部"}
                </Button>
              ))}
            </Space>
            <Table
              rowKey="ID"
              dataSource={feedbacks}
              locale={{ emptyText: "没有符合条件的反馈" }}
              columns={[
                {
                  title: "状态",
                  dataIndex: "Status",
                  width: 92,
                  render: (v: string) => (
                    <Tag color={v === "已处理" ? "green" : "orange"}>{v}</Tag>
                  ),
                },
                {
                  title: "用户编号",
                  dataIndex: "OwnerCode",
                  width: 110,
                  render: (v: string) => v || "-",
                },
                {
                  title: "反馈内容",
                  dataIndex: "Content",
                  render: (v: string) => (
                    <div style={{ whiteSpace: "pre-wrap", lineHeight: 1.6 }}>{v}</div>
                  ),
                },
                {
                  title: "处理人",
                  dataIndex: "HandledBy",
                  width: 100,
                  render: (v: string | null) => v || "-",
                },
                {
                  title: "备注",
                  dataIndex: "Note",
                  width: 180,
                  render: (v: string | null) => v || "-",
                },
                {
                  title: "提交时间",
                  dataIndex: "CreatedAt",
                  width: 180,
                  render: (v: string) => new Date(v).toLocaleString("zh-CN"),
                },
                {
                  title: "",
                  width: 120,
                  render: (_: unknown, r: FeedbackRow) => (
                    <Button
                      onClick={() => {
                        setFbEdit(r);
                        setFbNote(r.Note || "");
                      }}
                    >
                      {r.Status === "已处理" ? "退回待处理" : "标记已处理"}
                    </Button>
                  ),
                },
              ]}
            />
            <Modal
              open={!!fbEdit}
              title={fbEdit?.Status === "已处理" ? "退回待处理" : "标记已处理"}
              okText="确认"
              onCancel={() => setFbEdit(null)}
              onOk={async () => {
                if (!fbEdit) return;
                try {
                  await api(
                    "/feedbacks/" + fbEdit.ID,
                    {
                      Status: fbEdit.Status === "已处理" ? "待处理" : "已处理",
                      Note: fbNote,
                    },
                    "PUT",
                  );
                  setFbEdit(null);
                  await loadFeedbacks(fbStatus);
                } catch (e) {
                  report(e);
                }
              }}
            >
              <p style={{ marginTop: 0, color: "#888" }}>
                {fbEdit ? fbEdit.Content : ""}
              </p>
              <Input.TextArea
                rows={3}
                maxLength={500}
                value={fbNote}
                onChange={(e) => setFbNote(e.target.value)}
                placeholder="处理备注，选填"
              />
            </Modal>
          </>
        )}
        {page === "会员订单" && (
          <Table
            rowKey="ID"
            dataSource={orderRows}
            columns={[
              { title: "订单号", dataIndex: "ID" },
              { title: "用户", dataIndex: "Owner" },
              { title: "套餐天数", dataIndex: "Days" },
              {
                title: "订单金额（元）",
                render: (_, o: Order) => o.Price / 100,
              },
              { title: "状态", dataIndex: "Status" },
              {
                title: "",
                render: (_, o: Order) =>
                  o.Status === "待支付" ? (
                    <Button
                      onClick={async () => {
                        try {
                          await api("/orders/" + o.ID + "/reconcile", {});
                          await openPage("会员订单");
                        } catch (e) {
                          report(e);
                        }
                      }}
                    >
                      渠道查单
                    </Button>
                  ) : null,
              },
            ]}
          />
        )}
        {page === "会员管理" && (
          <>
            <Input.Search
              placeholder="按用户编号查（如 U3M8Q2K）"
              allowClear
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onSearch={() => void loadMembers().catch(report)}
            />
            <Table
              rowKey="ID"
              dataSource={members}
              columns={[
                {
                  title: "用户编号",
                  dataIndex: "Code",
                  render: (v: string, m: Member) => v || "#" + m.ID,
                },
                { title: "训练经验", dataIndex: "Experience" },
                {
                  title: "会员有效期",
                  render: (_, m: Member) =>
                    new Date(m.Expires) > new Date()
                      ? m.Expires.slice(0, 10)
                      : "未生效",
                },
                {
                  title: "",
                  render: (_, m: Member) => (
                    <Space>
                      {admin && (
                        <Button onClick={() => setMember(m)}>发放权益</Button>
                      )}
                      <Button
                        onClick={async () => {
                          try {
                            setGrants(
                              await api<Grant[]>(
                                "/members/" + m.ID + "/history",
                              ),
                            );
                            setGrantUser(m.ID);
                          } catch (e) {
                            report(e);
                          }
                        }}
                      >
                        权益流水
                      </Button>
                      {admin && (
                        <Button
                          danger
                          onClick={() => {
                            setPurgeCode("");
                            setPurge(m);
                          }}
                        >
                          注销
                        </Button>
                      )}
                    </Space>
                  ),
                },
              ]}
            />
            <Modal
              open={!!purge}
              title="注销账号"
              okText="确认注销"
              cancelText="取消"
              okButtonProps={{
                danger: true,
                disabled:
                  !purge ||
                  purgeCode.trim().toUpperCase() !==
                    (purge.Code || "").toUpperCase(),
              }}
              onCancel={() => setPurge(null)}
              onOk={async () => {
                if (!purge) return;
                try {
                  await api(
                    "/members/" + purge.ID,
                    { Code: purgeCode.trim() },
                    "DELETE",
                  );
                  setPurge(null);
                  await loadMembers();
                  void message.success("已注销");
                } catch (e) {
                  report(e);
                }
              }}
            >
              <p>
                注销后不可恢复：账号、计划、训练记录、反馈与人工权益流水会一起删除。
              </p>
              <p>
                已支付订单保留但脱敏（不再关联到具体用户），用于交易留档；账号还有待支付订单时不允许注销。
              </p>
              <p>
                请输入用户编号 <b>{purge?.Code}</b> 确认：
              </p>
              <Input
                value={purgeCode}
                onChange={(e) => setPurgeCode(e.target.value)}
                placeholder={purge?.Code}
              />
            </Modal>
          </>
        )}
        <Modal
          open={!!edit}
          title="配置周模板"
          onCancel={() => setEdit(null)}
          footer={null}
          destroyOnClose
          width={760}
        >
          {edit && (
            <Form
              key={edit.ID}
              initialValues={{
                ...edit,
                // Meta 可能为 null（早期模板），填一套空值让表单能从零开始编辑
                Meta: {
                  Cover: edit.Meta?.Cover || "",
                  Level: edit.Meta?.Level || "",
                  Minutes: edit.Meta?.Minutes || 0,
                  Kcal: edit.Meta?.Kcal || 0,
                  Blocks: edit.Meta?.Blocks || [],
                },
              }}
              layout="vertical"
              onFinish={submitPlan}
            >
              <Form.Item
                name="Name"
                label="模板名称"
                rules={[{ required: true }]}
              >
                <Input />
              </Form.Item>

              <Divider orientation="left" plain>
                小程序展示
              </Divider>
              <Form.Item
                name={["Meta", "Cover"]}
                label="封面图链接"
                extra="贴 https 图片地址，留空时小程序显示深色渐变块。图片所在域名要加进小程序后台的「downloadFile 合法域名」，否则真机加载不出来。"
              >
                <Input placeholder="https://example.com/cover.jpg" allowClear />
              </Form.Item>
              <Form.Item
                noStyle
                shouldUpdate={(a, b) => a.Meta?.Cover !== b.Meta?.Cover}
              >
                {({ getFieldValue }) => {
                  const src = getFieldValue(["Meta", "Cover"]) as
                    | string
                    | undefined;
                  return src ? (
                    <img className="cover-preview" src={src} alt="" />
                  ) : null;
                }}
              </Form.Item>
              <Space>
                <Form.Item name={["Meta", "Level"]} label="难度">
                  <Select
                    style={{ width: 120 }}
                    allowClear
                    options={["入门", "中级", "进阶"].map((v) => ({
                      value: v,
                      label: v,
                    }))}
                  />
                </Form.Item>
                <Form.Item name={["Meta", "Minutes"]} label="单次时长（分钟）">
                  <InputNumber min={0} max={300} />
                </Form.Item>
              </Space>
              <Form.List name={["Meta", "Blocks"]}>
                {(blocks, ops) => (
                  <>
                    {blocks.map((b) => (
                      <section key={b.key} className="meta-block">
                        <Form.Item label="小标题" name={[b.name, "Title"]}>
                          <Input placeholder="例如：计划介绍 / 计划结构 / 核心理念" />
                        </Form.Item>
                        <Form.Item label="正文" name={[b.name, "Text"]}>
                          <Input.TextArea
                            autoSize={{ minRows: 2 }}
                            placeholder="整段说明；填了要点就只显示要点"
                          />
                        </Form.Item>
                        <Form.Item
                          label="要点"
                          name={[b.name, "Lines"]}
                          // 存的是字符串数组，表单里用一个多行文本编辑，进出各转换一次
                          getValueProps={(v) => ({
                            value: ((v as string[]) || []).join("\n"),
                          })}
                          normalize={(v) =>
                            String(v)
                              .split("\n")
                              .map((s) => s.trim())
                              .filter(Boolean)
                          }
                          extra="一行一条；需要 ①②③ 就自己写在行首"
                        >
                          <Input.TextArea autoSize={{ minRows: 2 }} />
                        </Form.Item>
                        <Button onClick={() => ops.remove(b.name)}>
                          移除此段
                        </Button>
                      </section>
                    ))}
                    <Button
                      onClick={() =>
                        ops.add({ Title: "", Text: "", Lines: [] })
                      }
                      style={{ marginBottom: 20 }}
                    >
                      添加一段介绍
                    </Button>
                  </>
                )}
              </Form.List>

              <Divider orientation="left" plain>
                训练安排
              </Divider>
              <Form.List name="Days">
                {(days, { add, remove }) => (
                  <>
                    {days.map((d) => (
                      <section key={d.key}>
                        <Space>
                          <Form.Item name={[d.name, "Weekday"]} label="训练日">
                            <Select
                              options={Array.from({ length: 7 }, (_, i) => ({
                                value: i + 1,
                                label: "周" + "一二三四五六日"[i],
                              }))}
                            />
                          </Form.Item>
                          <Form.Item name={[d.name, "Focus"]} label="训练部位">
                            <Input />
                          </Form.Item>
                          <Button onClick={() => remove(d.name)}>
                            移除此日
                          </Button>
                        </Space>
                        <Form.List name={[d.name, "Items"]}>
                          {(items, ops) => (
                            <>
                              {items.map((i) => (
                                <div className="line" key={i.key}>
                                  <Form.Item
                                    label="动作"
                                    name={[i.name, "Name"]}
                                  >
                                    <Select
                                      showSearch
                                      style={{ minWidth: 130 }}
                                      options={catalog.map((x) => ({
                                        label: x.Name,
                                        value: x.Name,
                                      }))}
                                    />
                                  </Form.Item>
                                  {(["Sets", "Reps", "Rest"] as const).map(
                                    (k, n) => (
                                      <Form.Item
                                        key={k}
                                        name={[i.name, k]}
                                        label={["组数", "次数", "休息秒数"][n]}
                                      >
                                        {k === "Rest" ? (
                                          <Select
                                            options={[30, 60, 90].map(
                                              (value) => ({ value }),
                                            )}
                                          />
                                        ) : (
                                          <InputNumber min={1} />
                                        )}
                                      </Form.Item>
                                    ),
                                  )}
                                  <Button onClick={() => ops.remove(i.name)}>
                                    移除
                                  </Button>
                                </div>
                              ))}
                              <Button
                                onClick={() => ops.add(blankDay().Items[0])}
                              >
                                添加动作
                              </Button>
                            </>
                          )}
                        </Form.List>
                      </section>
                    ))}
                    <Button onClick={() => add(blankDay())}>添加训练日</Button>
                  </>
                )}
              </Form.List>
              <Space>
                <Button htmlType="submit" onClick={() => setIntent("draft")}>
                  保存草稿
                </Button>
                <Button
                  type="primary"
                  htmlType="submit"
                  onClick={() => setIntent("publish")}
                >
                  发布到小程序
                </Button>
                {edit.Published && (
                  <Button
                    danger
                    htmlType="submit"
                    onClick={() => setIntent("withdraw")}
                  >
                    下架
                  </Button>
                )}
              </Space>
            </Form>
          )}
        </Modal>
        <Modal
          open={!!grantUser}
          title="权益流水"
          footer={null}
          onCancel={() => setGrantUser(0)}
        >
          <Table
            rowKey="ID"
            dataSource={grants}
            columns={[
              { title: "天数", dataIndex: "Days" },
              { title: "来源", dataIndex: "Operator" },
              { title: "原因", dataIndex: "Reason" },
              {
                title: "状态",
                render: (_, g: Grant) =>
                  g.Revoked ? (
                    "已撤销"
                  ) : admin && g.Operator === "admin" ? (
                    <Button
                      onClick={() => {
                        setRevokeId(g.ID);
                        setReason("");
                      }}
                    >
                      撤销
                    </Button>
                  ) : (
                    "已到账"
                  ),
              },
            ]}
          />
        </Modal>
        <Modal
          open={!!revokeId}
          title="撤销人工权益"
          onCancel={() => setRevokeId(0)}
          onOk={async () => {
            try {
              await api(
                "/members/" + grantUser + "/grants/" + revokeId + "/revoke",
                { Reason: reason },
              );
              setRevokeId(0);
              setGrantUser(0);
              await loadMembers();
            } catch (e) {
              report(e);
            }
          }}
        >
          <Input
            placeholder="撤销原因"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Modal>
        <Modal
          open={!!member}
          title={"发放会员权益 · " + (member?.ID ?? "")}
          footer={null}
          onCancel={() => setMember(null)}
          destroyOnClose
        >
          {member && (
            <Form
              layout="vertical"
              onFinish={async (v: { Days: number; Reason: string }) => {
                try {
                  await api("/members/" + member.ID + "/grants", {
                    ...v,
                    Previous: member.Expires,
                  });
                  setMember(null);
                  await loadMembers();
                } catch (e) {
                  report(e);
                }
              }}
            >
              <Form.Item
                label="发放天数"
                name="Days"
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={365} />
              </Form.Item>
              <Form.Item
                label="发放原因"
                name="Reason"
                rules={[{ required: true }]}
              >
                <Input />
              </Form.Item>
              <Button type="primary" htmlType="submit">
                确认发放
              </Button>
            </Form>
          )}
        </Modal>
        <Modal
          open={!!entry}
          title={entry?.ID ? "编辑动作资料" : "新增动作"}
          footer={null}
          onCancel={() => setEntry(null)}
          destroyOnClose
          width={720}
        >
          {entry && (
            <ExerciseForm
              entry={entry}
              meta={meta}
              onDone={() => {
                setEntry(null);
                void openPage("训练资料").catch(report);
              }}
            />
          )}
        </Modal>
      </main>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <ConfigProvider
    locale={zhCN}
    theme={{ token: { colorPrimary: "#222820", borderRadius: 8 } }}
  >
    <Console />
  </ConfigProvider>,
);
