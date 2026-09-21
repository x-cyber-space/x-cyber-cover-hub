# X-Cyber-Cover-Hub

```
__  __   ____ y b e r   ____ o v e r _   _       _     
\ \/ /  / ___|         / ___|       | | | | _   | |__  
 \  /  | |      _____ | |           | |_| || |  | '_ \ 
 /  \  | |___  |_____|| |___        |  _  || |_ | |_) |
/_/\_\  \____|         \____|       |_| |_| \___||_.__/ 
             :: X-Cyber Cover Hub :: [v1.0.0]
             :: Album Artwork Proxy ::
```

`x-cyber-cover-hub` 是一个**本地自建的专辑封面聚合与中转服务**，为音乐播放器提供"给定歌手 + 专辑，拿回一张干净的封面图"。

它是 [`x-cyber-lrc-hub`](https://github.com/x-cyber-space/x-cyber-lrc-hub) 的姊妹项目：歌词服务解决"歌词不对"，这个解决"封面不对"。

- **按专辑缓存，不是按歌曲** —— 一张 12 首的专辑只需要抓一次、存一份。这是它存在的首要理由（见下）。
- **两个源，自动兜底** —— iTunes 为主（免费、无 key、**无防盗链**、可任意改分辨率），网易云为备（覆盖 iTunes 缺失的中文曲库）。
- **先匹配再取图** —— 按 (歌手, 专辑) 判定是不是同一张唱片，宁可返回 `404`，也不把错封面写进你的音频文件。
- **字节落地** —— 直接返回图片本身，不用给客户端一个会被 403 的第三方 URL。
- **零 CGO** —— 纯 Go SQLite，单一静态二进制。

---

## 为什么需要它

如果你用的是本项目作者的音乐播放器，它原本的在线封面逻辑有两个问题：

**1. 缓存键是 per-song，不是 per-album**

```kotlin
File(dir, "cover_$songId.jpg")   // 12 首歌的一张专辑 → 抓 12 次、存 12 份相同的图
```

**2. `limit=1` 盲取第一条搜索结果，完全没有匹配**

```kotlin
"https://itunes.apple.com/search?term=$encodedQuery&entity=song&limit=1"
// 拿到 artworkUrl100 就当成答案，从不核对专辑名
```

而封面一旦被写进音频文件的 ID3/FLAC 标签，就是**永久的**——错了要人工去改。所以这个服务的取舍和歌词服务一样：**宁可 404，不可猜错**。

把它放进本地服务，顺带解决的还有：一张专辑只抓一次、多首歌并发请求只落一次、封面与音频文件解耦（不必为了换封面而改写文件）。

---

## 快速上手

### 1. 编译

系统要求：`Go 1.25+`（下限来自 `modernc.org/sqlite`）。

```bash
git clone https://github.com/x-cyber-space/x-cyber-cover-hub.git
cd x-cyber-cover-hub
make build          # 产物在 bin/x-cyber-cover-hub
```

### 2. 启动

```bash
# 默认端口 3400，缓存 ./data/covers.db，默认封面边长 600px
./bin/x-cyber-cover-hub

# 自定义
./bin/x-cyber-cover-hub -port 3400 -cache ./data/covers.db -size 600 -cache-ttl 2160h
```

| 参数 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `-port` | int | `3400` | 服务监听端口 |
| `-cache` | string | `./data/covers.db` | SQLite 缓存路径 |
| `-cache-ttl` | duration | `2160h`（90 天） | 缓存有效期；`0` 表示永不过期 |
| `-size` | int | `600` | 请求未带 `?size` 时的默认封面边长 |
| `-log-level` | string | `info` | `debug` / `info` / `warn` / `error` |

### 3. Docker

```bash
make docker
docker run -d --name cover-hub -p 3400:3400 -v cover-data:/data x-cyber-cover-hub:latest
```

基于 `alpine`，以非 root 用户（uid 10001）运行，缓存持久化在 `/data` 卷上。

---

## 接口

### `GET /api/cover`

| 参数 | 必需 | 说明 |
|---|---|---|
| `artist_name` | ✅ | 歌手名 |
| `album_name` | ⚠️ | 专辑名。**强烈建议传**——它是封面的身份；不传就只能靠歌手瞎猜，且无法走缓存 |
| `track_name` | | 歌曲名。仅在**没有** `album_name` 时用于搜索 |
| `size` | | 封面边长像素，`64`–`3000`，默认取 `-size` |
| `format` | | 传 `json` 返回元数据而不是图片 |

**默认返回图片字节**：

```bash
curl -sS "http://localhost:3400/api/cover?artist_name=周杰伦&album_name=叶惠美" \
  -o cover.jpg -D -
```

```
HTTP/1.1 200 OK
Content-Type: image/jpeg
ETag: "3f2a...c1"
Cache-Control: public, max-age=604800
X-Cover-Source: itunes
X-Cover-Artist: %E5%91%A8%E6%9D%B0%E4%BC%A6
X-Cover-Album: %E5%8F%B6%E6%83%A0%E7%BE%8E
```

> `X-Cover-Artist` / `X-Cover-Album` 是 **percent-encoded** 的：HTTP 头必须是 ASCII，而中英文专辑名不是。要看明文用 `?format=json`。

**`?format=json` 返回元数据**：

```json
{
  "artistName": "周杰伦",
  "albumName": "叶惠美",
  "source": "itunes",
  "contentType": "image/jpeg",
  "size": 600,
  "byteSize": 48213,
  "etag": "\"3f2a...c1\"",
  "artworkUrl": "https://is1-ssl.mzstatic.com/image/thumb/.../600x600bb.jpg",
  "imageUrl": "/api/cover?album_name=%E5%8F%B6%E6%83%A0%E7%BE%8E&artist_name=%E5%91%A8%E6%9D%B0%E4%BC%A6&size=600"
}
```

`imageUrl` 指向本服务——**这才是客户端该取的那个 URL**。`artworkUrl` 是上游地址，多数情况下客户端直连会被拒（防盗链），仅供参考。

**错误响应**：

| 状态码 | `name` | 场景 |
|---|---|---|
| `400` | `BadRequest` | 缺 `artist_name`，或既没有 `album_name` 也没有 `track_name`，或 `size` 越界 |
| `404` | `CoverNotFound` | 没有源能**确信**地匹配到这张专辑 |
| `405` | — | 非 GET，空响应体 |
| `503` | `UpstreamUnavailable` | 所有源都请求失败（上游故障，不是"没有这张专辑"） |

### `GET /health`

```json
{"status": "ok"}
```

---

## 匹配策略

封面身份 = **(歌手, 专辑)**。这和歌词服务正相反：歌词按 (歌名, 歌手, 时长) 匹配，而一张专辑下十几首歌共享同一张封面，**歌名和时长对封面毫无意义**。

**第 1 级 —— 归一化精确匹配**
归一化 = 转小写 + 剥离括号修饰词 + 折叠空白。所以 `叶惠美` 与 `叶惠美 (Deluxe Edition)`、`1989` 与 `1989 (Taylor's Version)` 是**同一张专辑**——版次标记不该导致重复抓取。

**第 2 级 —— 受控放宽**
- 多歌手署名集合匹配：请求 `周杰伦` 可匹配平台署名 `周杰伦 / 费玉清`
- 专辑名包含关系或 Bigram 相似度 ≥ 0.6

**都不命中 → `404`。绝不返回"搜索结果第一条"。**

排序时**专辑权重（60）高于歌手（40）**——因为一个歌手有十几张封面，认错专辑才是要防的失败模式。

---

## 缓存

- **键**：`sha256(归一化歌手 \0 归一化专辑 \0 边长)[:16]`
- **存二进制**：图片字节进 SQLite BLOB，带 `Content-Type` 与 `byte_size`
- **命中要复核**：缓存命中会用**与冷启动相同的匹配谓词**重新校验，所以躺错位置的记录不会被返回
- **请求带 `track_name` 但无 `album_name` 时**：不查缓存（没有专辑就没有身份，所有专辑会撞成一行），但会**按解析出的真实身份写入**，供后续带专辑名的请求命中
- **过期**：默认 90 天，读取时判定，后台定期回收
- **并发**：连接数固定为 1，启用 WAL + `synchronous=NORMAL`

---

## 免责声明 (Disclaimer)

1. 本项目（`x-cyber-cover-hub`）为一个遵循 AGPL-3.0 协议的开源技术研究与学习项目，旨在为局域网媒体服务器及个人音乐播放器提供本地封面中转与格式适配功能。
2. 本项目不提供任何在线服务器，不进行任何商业牟利行为。**服务运行在你自己的机器上，所有请求由你自己的网络发起。**
3. **专辑封面是独立的美术作品**，其版权归属于原美术作品权利人、摄影师、设计师或唱片公司。相关商标归各唱片公司所有。本软件检索的数据仅供个人学习、研究与欣赏，严禁用于任何商业用途。
4. 本服务会**在其本地缓存中保存图片字节**（这是它能绕过防盗链、并为同一专辑多首歌复用一份数据的原因）。使用者应自行了解并遵守所在地区的著作权法律法规及第三方平台的服务协议。
5. 开发者对使用者因不正当使用所引发的任何直接或间接纠纷不承担任何法律责任。

---

## 开源协议

[GNU Affero General Public License v3.0 (AGPL-3.0)](./LICENSE)
