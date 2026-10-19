# tchat

윈도우·맥·리눅스 터미널에서 쓰는 채팅 프로그램입니다.
별명을 정하고, 방을 만들거나 골라서 들어가 대화합니다.

- 방에 누가 있는지 보기(옆 패널, `/who`)와 별명 바꾸기(`/nick`)를 지원합니다.
- **대화는 저장되지 않습니다.** 서버는 메시지를 전달만 하고 버리며, 마지막 사람이 나가면 방도 사라집니다. 클라이언트도 대화를 화면과 메모리에만 둡니다.
- 메시지의 제어 문자는 제거되므로, 이스케이프 시퀀스로 남의 터미널을 망가뜨릴 수 없습니다.
- 기본 도배 방지: 초당 1개 정도, 순간적으로는 5개까지 보낼 수 있습니다.

## 사용자용

**macOS / Linux** (터미널)
```sh
curl -fsSL https://raw.githubusercontent.com/yeongseoo/tchat/main/scripts/install.sh | sh
tchat
```

**Windows** (PowerShell)
```powershell
irm https://raw.githubusercontent.com/yeongseoo/tchat/main/scripts/install.ps1 | iex
tchat
```

다른 서버에 접속하려면: `tchat chat.example.com`

| 위치 | 키 / 명령 | 동작 |
|---|---|---|
| 로비 | ↑↓, Enter | 방 선택 후 입장 |
| 로비 | `n` | 새 방 만들기 |
| 로비 | `r` / `q` | 목록 새로고침 / 종료 |
| 채팅 | `/nick 새별명` | 별명 바꾸기 |
| 채팅 | `/who` | 접속자 목록 |
| 채팅 | `Esc` 또는 `/leave` | 방 나가기 |
| 채팅 | PgUp / PgDn | 이전 대화 스크롤 |
| 어디서나 | Ctrl+C | 종료 |

## 운영자용

### 로컬에서 실행
```sh
go run . serve            # 서버 (:8080)
go run . localhost:8080   # 다른 터미널에서 클라이언트로 접속
```

### 서버 배포
서버는 실행파일 하나이고 데이터베이스가 필요 없습니다.

```sh
docker build -t tchat .
docker run -p 8080:8080 tchat
```
Fly.io, Railway, Render 같은 호스팅 서비스에 이 저장소를 연결하면 Dockerfile로 빌드됩니다.
`PORT` 환경변수가 있으면 자동으로 그 포트를 씁니다.
사용자는 `wss://`로 접속하므로 HTTPS로 서비스해야 합니다. 대부분의 호스팅 서비스가 자동으로 해줍니다.

### 클라이언트 배포 (릴리스)
1. `scripts/install.sh`, `scripts/install.ps1`, 이 README의 `OWNER`를 본인 GitHub 아이디로 바꿉니다.
2. GitHub 저장소의 Settings → Secrets and variables → Actions → **Variables**에 `TCHAT_SERVER` = `wss://내서버주소/ws`를 추가합니다.
   이 값이 기본 서버로 들어가서, 사용자는 주소 없이 `tchat`만 입력하면 됩니다.
3. 태그를 push하면 GitHub Actions가 윈도우·맥·리눅스용 실행파일을 빌드해서 Release에 올립니다.
   ```sh
   git tag v0.1.0 && git push origin v0.1.0
   ```

## 구조
```
main.go                     명령 처리 (serve / 접속)
internal/protocol           JSON 메시지 정의
internal/server             WebSocket 서버 (메모리 전용)
internal/client             터미널 UI (bubbletea)
```
