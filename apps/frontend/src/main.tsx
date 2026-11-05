import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Button,
  ConfigProvider,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
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
}
interface Product {
  Days: number;
  Price: number;
  Enabled: boolean;
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
  Experience: string;
  Expires: string;
}
interface Exercise {
  ID: number;
  Name: string;
  Muscle: string;
  Equipment: string;
  Guide: string;
  Setup: string;
}
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
function Console(): React.JSX.Element {
  const [logged, setLogged] = useState(false),
    [page, setPage] = useState("训练模板"),
    [plans, setPlans] = useState<Plan[]>([]),
    [members, setMembers] = useState<Member[]>([]),
    [edit, setEdit] = useState<Plan | null>(null),
    [member, setMember] = useState<Member | null>(null),
    [intent, setIntent] = useState("draft"),
    [orderRows, setOrderRows] = useState<Order[]>([]),
    [grants, setGrants] = useState<Grant[]>([]),
    [grantUser, setGrantUser] = useState(0),
    [revokeId, setRevokeId] = useState(0),
    [reason, setReason] = useState(""),
    [products, setProducts] = useState<Product[]>([]),
    [admin, setAdmin] = useState(false),
    [audit, setAudit] = useState<Audit[]>([]),
    [catalog, setCatalog] = useState<Exercise[]>([]),
    [entry, setEntry] = useState<Exercise | null>(null),
    [query, setQuery] = useState("");
  async function refresh(): Promise<void> {
    const rows = await api<Plan[]>("/templates");
    setPlans(rows);
    setCatalog(await api<Exercise[]>("/exercises"));
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
  async function openPage(p: string): Promise<void> {
    if (p === "操作记录") setAudit(await api<Audit[]>("/audit"));
    if (p === "会员套餐") setProducts(await api<Product[]>("/products"));
    if (p === "会员订单") setOrderRows(await api<Order[]>("/orders"));
    if (p === "会员管理") await loadMembers();
    if (p === "训练资料" || p === "训练模板")
      setCatalog(await api<Exercise[]>("/exercises"));
    setPage(p);
  }
  async function submitPlan(values: Plan): Promise<void> {
    try {
      await api("/templates", { ...edit, ...values, Intent: intent });
      await refresh();
      setEdit(null);
    } catch (e) {
      report(e);
    }
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
                })
              }
            >
              新建周模板
            </Button>
            <Table
              rowKey="ID"
              dataSource={plans}
              columns={[
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
                    <Button onClick={() => setEdit(p)}>配置</Button>
                  ),
                },
              ]}
            />
          </>
        )}
        {page === "训练资料" && (
          <>
            <Button
              onClick={() =>
                setEntry({
                  ID: 0,
                  Name: "",
                  Muscle: "",
                  Equipment: "",
                  Setup: "",
                  Guide: "",
                })
              }
            >
              新增动作
            </Button>
            <Table
              rowKey="ID"
              dataSource={catalog}
              columns={[
                { title: "动作", dataIndex: "Name" },
                { title: "肌群", dataIndex: "Muscle" },
                { title: "器材", dataIndex: "Equipment" },
                {
                  title: "",
                  render: (_, x: Exercise) => (
                    <Button onClick={() => setEntry(x)}>编辑</Button>
                  ),
                },
              ]}
            />
          </>
        )}
        {page === "会员套餐" && (
          <Table
            rowKey="Days"
            dataSource={products}
            columns={[
              { title: "有效天数", dataIndex: "Days" },
              { title: "售价（元）", render: (_, p: Product) => p.Price / 100 },
              {
                title: "状态",
                render: (_, p: Product) => (p.Enabled ? "已上架" : "已下架"),
              },
              {
                title: "",
                render: (_, p: Product) => (
                  <Button
                    disabled={!admin}
                    onClick={async () => {
                      try {
                        await api(
                          "/products/" + p.Days,
                          { Enabled: !p.Enabled },
                          "PUT",
                        );
                        await openPage("会员套餐");
                      } catch (e) {
                        report(e);
                      }
                    }}
                  >
                    {p.Enabled ? "下架" : "上架"}
                  </Button>
                ),
              },
            ]}
          />
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
              placeholder="按用户 ID 查询"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onSearch={() => void loadMembers().catch(report)}
            />
            <Table
              rowKey="ID"
              dataSource={members}
              columns={[
                { title: "用户 ID", dataIndex: "ID" },
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
                    </Space>
                  ),
                },
              ]}
            />
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
              initialValues={edit}
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
          title="动作资料"
          footer={null}
          onCancel={() => setEntry(null)}
          destroyOnClose
        >
          {entry && (
            <Form
              initialValues={entry}
              layout="vertical"
              onFinish={async (v: Exercise) => {
                try {
                  await api("/exercises", { ...entry, ...v });
                  await openPage("训练资料");
                  setEntry(null);
                } catch (e) {
                  report(e);
                }
              }}
            >
              {(
                [
                  "Name",
                  "Muscle",
                  "Equipment",
                  "Setup",
                  "Guide",
                  "AuxMuscles",
                  "Mistakes",
                  "ImageURL",
                ] as const
              ).map((k, n) => (
                <Form.Item
                  key={k}
                  name={k}
                  label={
                    [
                      "动作名称",
                      "目标肌群",
                      "器材",
                      "器材调节",
                      "动作步骤",
                      "辅助肌群",
                      "常见错误",
                      "教学图 HTTPS 地址",
                    ][n]
                  }
                  rules={[{ required: n < 5 }]}
                >
                  <Input />
                </Form.Item>
              ))}
              <Button htmlType="submit" type="primary">
                保存资料
              </Button>
            </Form>
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
