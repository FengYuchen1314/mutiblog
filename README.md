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
- Docker Compose 插件；
- 一台 amd64 或 arm64 Linux 服务器。

部署直接使用 GitHub Container Registry 中预构建的镜像，不需要下载源码，也不需要在服务器上编译。

### 1. 创建部署目录

```bash
mkdir -p ~/mutiblog
cd ~/mutiblog
```

也可以换成任意其他目录。Docker Compose 会以当前目录作为这个 MutiBlog 实例的项目目录。

### 2. 创建 Compose 文件

在该目录中新建 `compose.yaml`：

```yaml
services:
  prepare-data:
    image: ghcr.io/fengyuchen1314/mutiblog:latest
    pull_policy: always
    user: "0:0"
    entrypoint: ["chown", "-R", "1000:1000", "/var/lib/mutiblog"]
    volumes:
      - ./data:/var/lib/mutiblog

  mutiblog:
    image: ghcr.io/fengyuchen1314/mutiblog:latest
    pull_policy: always
    depends_on:
      prepare-data:
        condition: service_completed_successfully
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/var/lib/mutiblog
```

该文件只使用 GitHub 预构建镜像，不包含 `build`，因此不会在本机编译 MutiBlog。

`prepare-data` 会在首次启动时创建并修正 `./data` 的权限，随后由 `mutiblog` 以非 root 用户运行。配置和全部站点数据都会保存在当前部署目录中：

```text
mutiblog/
├── compose.yaml
└── data/
```

默认直接开放服务器的 `8080` 端口。如需使用其他端口，只修改冒号左侧的数字，例如使用 `9000`：

```yaml
ports:
  - "9000:8080"
```

### 3. 启动

```bash
docker compose up -d
```

Docker Compose 会自动完成以下操作：

1. 从 `ghcr.io` 拉取适合当前服务器架构的最新镜像；
2. 在当前目录创建 `data` 数据目录并设置正确权限；
3. 创建并启动 MutiBlog 容器；
4. 配置容器随 Docker 自动重启。

查看状态和日志：

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

进入保存 `compose.yaml` 的部署目录，然后拉取最新预构建镜像并替换容器：

```bash
cd ~/mutiblog
docker compose pull
docker compose up -d
```

更新不会删除已有数据。

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

`docker compose down` 只会停止并移除容器，不会删除部署目录中的 `data`。不要在未备份时手动删除该目录。

## 许可证

MutiBlog 使用 [AGPL-3.0](LICENSE) 许可证。第三方组件保留各自许可证，详情见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
