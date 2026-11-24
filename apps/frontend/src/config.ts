// 图片 CDN 前缀。
//
// 动作示范图（ImageURL）、计划封面（Meta.Cover）在库里只存相对路径（/assets/...），
// 域名集中在这一个文件里：换 CDN 只改 CDN_IMG 这一行，数据和小程序都不用动。
// 小程序那边对应的是 self-reliant-gym/config/env.js 里的 CDN_IMG，两处填同一个域名。
//
// 留空时 <img> 拿到的是站内相对路径，而后台静态目录里并没有 /assets/exercises 这些图，
// 所以后台预览一定是 404 —— 线上必须填域名，和小程序那边保持同一个。
export const CDN_IMG = "https://cdn.voiddiver.cn";

// 把库里存的相对路径拼成能直接放进 <img src> 的完整地址。
// 已带协议的（老数据里填过的完整 http/https 链接）、协议相对的地址原样返回，
// 否则会把 https:// 拼成 https://cdn/https://…
export function imgUrl(path?: string | null): string {
  const v = (path || "").trim();
  if (!v) return "";
  if (/^[a-z][a-z0-9+.-]*:/i.test(v) || v.startsWith("//")) return v;
  const suffix = v.startsWith("/") ? v : "/" + v;
  return CDN_IMG.replace(/\/+$/, "") + suffix;
}

// 表单校验用：留空、https 地址、站内相对路径都算合法；
// http 不给过——真机上会被微信拦截，这也是之前要求填 https 的原因。
export function checkImagePath(path: string): string {
  const v = (path || "").trim();
  if (!v) return "";
  if (/^https:\/\//i.test(v)) return "";
  if (/^[a-z][a-z0-9+.-]*:/i.test(v)) return "请填相对路径或用 https 地址";
  return "";
}
