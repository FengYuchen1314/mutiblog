# MutiBlog

MutiBlog 是一个面向个人创作者的单站点、单管理员、自托管发布系统。

## 主要特性

- 使用 Markdown 编写内容，并支持粘贴图片直接上传；
- 以 Markdown 和 YAML 文件作为持久化数据源；
- 支持 AI 多语言翻译、人工校对和确定性的语言回退；
- 公开站点采用静态生成，评论作为可选的动态功能；
- 提供 Vue 3 管理后台；
- 支持可安装的 React SSR 主题，并内置 Earth 主题；
- 单容器运行，资源占用较低；
- 支持 amd64 和 arm64。

旧版实现保存在 `old` 分支，仅供归档，不作为当前版本的实现参考。

## Docker Compose 部署

### 环境要求

- Docker Engine 24 或更高版本；
- Docker Compose 插件。

确认环境可用：

```bash
docker version
docker compose version
```

### 1. 获取代码

```bash
git clone https://github.com/FengYuchen1314/mutiblog.git
cd mutiblog
```

### 2. 配置访问端口

仓库中的 `compose.yaml` 默认只监听本机的 `127.0.0.1:8080`：

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

如果需要直接通过服务器公网地址访问，将其改为：

```yaml
ports:
  - "8080:8080"
```

如需使用其他宿主机端口，只修改冒号左侧的数字，例如：

```yaml
ports:
  - "9000:8080"
```

### 3. 构建并启动

```bash
docker compose up -d --build
```

查看运行状态：

```bash
docker compose ps
docker compose logs -f mutiblog
```

容器状态正常后，访问：

- 管理后台：`http://服务器地址:8080/console/`
- 健康检查：`http://服务器地址:8080/health/ready`

首次访问管理后台会进入初始化页面。完成站点名称、基础地址、语言、时区和管理员账号设置后，系统会生成首个公开站点版本。

如果修改了宿主机端口，请将上述地址中的 `8080` 替换为实际端口。

## 更新

```bash
git pull --ff-only
docker compose up -d --build
```

更新会重新构建并替换容器，已有数据不会被删除。

## 常用命令

```bash
# 查看状态
docker compose ps

# 查看日志
docker compose logs -f mutiblog

# 重启
docker compose restart mutiblog

# 停止并移除容器
docker compose down
```

站点数据保存在 Docker 卷 `mutiblog-data` 中。`docker compose down` 不会删除该卷；不要在未备份数据时执行 `docker compose down -v`。

## 许可证

MutiBlog 使用 [AGPL-3.0](LICENSE) 许可证。第三方组件保留各自许可证，详情见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
