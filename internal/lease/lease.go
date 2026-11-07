// Package lease 提供「同一份配置只允许一个进程在写」的单飞租约。
//
// 为什么需要它：ANI-RSS 的 setAni 是**全量覆盖**语义。两个进程同时跑时，
// 各自读一份 listAni、各算一份目标状态、各自整份写回，后写的那份会盖掉先写的
// —— 于是两边都以为自己的规则生效了，实例里却是两个周期交替打架的结果。
// 这种问题不会报错，只在「主 RSS 明明该是 A 组，过一会儿又变回 B 组」时露头。
//
// # 互斥怎么保证：原子独占创建，不是「写完之后回读核对」
//
// 第一版是「读文件 → 判过期 → 写 → 回读确认文件里是我」。那**不成立**：
// 回读发生在自己写完之后、别人写之前的那个微秒级窗口里，8 个进程同时抢时
// 实测 8 个全都以为自己抢到了。回读能发现的只是「我写完之后立刻被别人盖掉」，
// 而那时候我已经返回成功了。
//
// 现在改成两步，每一步都只有一个赢家：
//
//  1. 快路径：`O_CREATE|O_EXCL` 独占创建。文件系统保证只有一个进程能成功。
//  2. 接管路径：判定期租约过期后，先 `os.Rename` 把旧文件改名让位 ——
//     改名是原子的，且源文件只剩一个，所以只有一个人能改成功；
//     改名成功的人回到第 1 步重新抢 `O_EXCL`，输了就报 ErrHeld。
//
// # 为什么是心跳租约而不是操作系统文件锁
//
// flock / LockFileEx 的释放靠句柄回收，要引平台 API（本项目发布物是单文件、
// 无运行时依赖的静态二进制）；心跳租约的释放靠时间，进程被强杀时自动过期，
// 反而更贴合「无人值守时没人来收尾」这个场景。
//
// 代价要说清楚：过期判断靠时间，不是靠进程存活。所以 TTL 必须大于
// 「一次心跳到下一次心跳」的最坏间隔（KeepAlive 会把它压到分钟级），
// 并且被强杀后最多要等一个 TTL 才能被别人接管 —— 急的时候用 force。
package lease

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zzzwannasleep/anisync/internal/fsutil"
)

// ErrHeld 表示租约被另一个仍然活着的持有者占着。
var ErrHeld = errors.New("租约被占用")

// ErrLost 表示本进程已经不是持有者：租约被接管、或者文件被改坏。
//
// 拿到它的调用方**必须停止写入**。这是「发现自己被接管」的唯一信号，
// 不处理就等于两个进程在写同一批订阅。
var ErrLost = errors.New("租约已不属于本进程")

// ErrHeartbeat 表示「这一次心跳没写进文件」，但**租约仍然属于本进程**。
//
// 它和 ErrLost 必须分开，因为两者正确的反应是相反的：
//
//	ErrLost     -> 立刻停写（别人已经接管，继续写就是双写覆盖）
//	ErrHeartbeat-> 下一拍接着心跳就行，**绝不能停写**
//
// 为什么值得单列一个错误：以前调用方只能看到「Renew 返回了非 nil」，
// 于是一次瞬时占用（Windows 上 32/5，磁盘忙、被别的进程短暂打开）
// 会让常驻进程**错判自己被接管然后静默停止写入** ——
// 而「一直失败」和「根本没在跑」在用户眼里本来就一样，
// 这正是 README 里专门警告过的那种坑。
//
// 注意：真正落后于 fsutil 的重试预算、写确实失败时才会拿到它 ——
// fsutil 内部已经会把「瞬时占用」等掉（见 internal/fsutil 的两个预算）。
var ErrHeartbeat = errors.New("租约心跳写入失败（租约仍归本进程）")

// HeartbeatError 是「心跳没写成」的具体错误，包着底层原因。
//
// 用类型而不是 fmt.Errorf("%w: %v") 是为了两头都保住：
// `errors.Is(err, ErrHeartbeat)` 判类别，`errors.Is/As(err, <底层错误>)` 查原因。
type HeartbeatError struct {
	Err error
}

func (e *HeartbeatError) Error() string {
	return "租约心跳写入失败（租约仍归本进程）：" + e.Err.Error()
}

// Unwrap 让 errors.Is / errors.As 能继续往下钻到底层错误。
func (e *HeartbeatError) Unwrap() error { return e.Err }

// Is 让 errors.Is(err, ErrHeartbeat) 成立。
func (e *HeartbeatError) Is(target error) bool { return target == ErrHeartbeat }

// Info 是租约文件的内容。
type Info struct {
	// PID 是持有者进程号，只用于给人看和排障。
	PID int `json:"pid"`
	// Host 是机器名：多机挂同一份网络盘时能看出是谁。
	Host string `json:"host,omitempty"`
	// Token 是本次持有者的随机标识，用来判断「文件里这条是不是我自己写的」。
	Token string `json:"token"`
	// StartedAt 是这次持有开始的时间。
	StartedAt time.Time `json:"startedAt"`
	// Heartbeat 是最后一次续约时间，过期判断看它。
	Heartbeat time.Time `json:"heartbeat"`
	// Note 说明持有者在干什么，比如 "tray" / "sync"。
	Note string `json:"note,omitempty"`
}

// Age 返回最后一次心跳距今多久。
func (i Info) Age(now time.Time) time.Duration {
	if i.Heartbeat.IsZero() {
		return time.Duration(1<<62 - 1)
	}
	return now.Sub(i.Heartbeat)
}

// Lease 是已持有的租约。
type Lease struct {
	path  string
	ttl   time.Duration
	info  Info
	nowFn func() time.Time
}

// acquireAttempts 是「抢租约」的轮数上限。
//
// 3 轮足够覆盖争抢：每轮要么拿到 O_EXCL、要么发现被占、要么把过期文件改名让位。
// 超过 3 轮还拿不到，说明有人正在这一瞬间反复进出，报 ErrHeld 比无限重试安全。
const acquireAttempts = 3

// startingWindow 是「文件刚被创建、内容还没写完」的判据窗口。
//
// 这个窗口里最可能是另一个进程正在正常启动，所以提示要等而不是 --force。
const startingWindow = 5 * time.Second

// Acquire 尝试拿到租约。
//
// 文件不存在时靠 O_EXCL 独占创建；已存在时先判过期，过期（或 force）就把它
// 改名让位，然后回到独占创建那一步重新抢。任何情况下都只有一个赢家。
func Acquire(path string, ttl time.Duration, note string, force bool) (*Lease, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt < acquireAttempts; attempt++ {
		now := time.Now()
		info := Info{
			PID:       os.Getpid(),
			Host:      host,
			Token:     token,
			StartedAt: now,
			Heartbeat: now,
			Note:      note,
		}

		// 第 1 步：原子独占创建。这一步就是互斥本身。
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			werr := writeInfoTo(file, info)
			cerr := file.Close()
			if werr != nil {
				_ = os.Remove(path)
				return nil, werr
			}
			if cerr != nil {
				_ = os.Remove(path)
				return nil, cerr
			}
			return &Lease{path: path, ttl: ttl, info: info, nowFn: time.Now}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}

		// 第 2 步：文件已存在。还活着就拒绝，过期就让位。
		//
		// 注意「读不出内容」这一支：文件存在但内容读不出来，有两种可能 ——
		//   (a) 上一个进程写到一半崩了 / 被手改坏了；
		//   (b) 另一个进程刚 O_EXCL 创建成功、还没把内容写进去。
		// 两者从外部看一模一样。判据只能退到文件年龄：新文件按「活着」处理，
		// 否则 (b) 会被当成过期租约抢走，于是又是两个赢家（实测 16 并发里出过 2 个）。
		current, ok := Holder(path)
		age := current.Age(now)
		if !ok {
			stamped, err := os.Stat(path)
			if err != nil {
				// 文件刚好在这个瞬间被删掉了：回到循环开头重新抢 O_EXCL。
				continue
			}
			age = now.Sub(stamped.ModTime())
		}
		if age < ttl && !force {
			if ok {
				return nil, fmt.Errorf("%w：pid %d（%s）最后一次心跳 %s 前，租约还剩 %s；"+
					"确认那个进程已经不再写入了，就加 --force 接管",
					ErrHeld, current.PID, describeNote(current.Note),
					age.Round(time.Second), (ttl - age).Round(time.Second))
			}
			// 这个分支最可能是「另一个进程刚 O_EXCL 创建成功、还没把内容写完」，
			// 也就是**有人正在正常启动**。此时提示 --force 会把人推向双写者，
			// 所以先说「等一下重试」。
			if age < startingWindow {
				return nil, fmt.Errorf("%w：租约文件刚被创建（%s 前）但内容还没写完，"+
					"多半是另一个进程正在启动；等几秒再试，不要用 --force",
					ErrHeld, age.Round(time.Millisecond))
			}
			return nil, fmt.Errorf("%w：租约文件读不出持有者，但 %s 前被写过（可能是上一个进程写坏了）；"+
				"确认没有别的进程在跑，再加 --force 接管",
				ErrHeld, age.Round(time.Second))
		}
		// 让位用改名而不是删除：改名是原子的，源文件只有一个，
		// 所以「多个进程同时判定过期」这一情形下也只有一个人能把旧文件挪走。
		retired := fmt.Sprintf("%s.stale.%d.%d", path, os.Getpid(), now.UnixNano())
		if err := os.Rename(path, retired); err != nil {
			// 别人先挪走了：回到循环开头，谁先抢到 O_EXCL 谁就是持有者。
			continue
		}
		_ = os.Remove(retired)
	}

	return nil, fmt.Errorf("%w：连续 %d 轮都没抢到（有别的进程正在反复进出）", ErrHeld, acquireAttempts)
}

// Info 返回本次持有的信息。
func (l *Lease) Info() Info { return l.info }

// Path 返回租约文件路径。
func (l *Lease) Path() string { return l.path }

// Renew 续约：先确认租约还属于自己，再把心跳时间写回去。
//
// 必须先校验：老进程被挂起/机器休眠超过 TTL 之后租约可能已经被别人接管，
// 它醒过来若无脑续约，就会把文件重新写成自己的名字 —— 而新持有者毫不知情，
// 于是又变成两个写者。校验之后这种情况表现为 ErrLost，调用方应当停写。
//
// 两种失败**必须分得开**（调用方看到的是 errors.Is）：
//
//	errors.Is(err, ErrLost)      -> 真的丢了：停写
//	errors.Is(err, ErrHeartbeat) -> 只这一次心跳没写成：下一拍接着续，不许停写
func (l *Lease) Renew() error {
	if l == nil {
		return nil
	}
	current, ok := Holder(l.path)
	if !ok {
		return fmt.Errorf("%w：租约文件不见了或内容无法解析", ErrLost)
	}
	if current.Token != l.info.Token {
		return fmt.Errorf("%w：现在是 pid %d（%s）持有", ErrLost, current.PID, describeNote(current.Note))
	}
	l.info.Heartbeat = l.nowFn()
	if err := writeInfo(l.path, l.info); err != nil {
		return &HeartbeatError{Err: err}
	}
	return nil
}

// KeepAlive 在后台定期续约，返回停止函数。
//
// 一轮同步可能跑十几分钟，而 TTL 只有几分钟；只在两轮之间续约会让
// 「正在认真干活」的进程被别人判成已死，所以续约必须是独立的节奏。
//
// onLost 会在**发现自己已不是持有者**时被调用一次（之后停止续约）。
// 调用方必须据此**停掉写入**：那是「我被人接管了」的唯一信号。
//
// onTransient（可选）在「这次心跳没写进去、但租约还是我的」时被调用，
// 之后**继续续约**。这个区分是必须的：把写失败当成丢租约，
// 会让常驻进程因为一次瞬时占用就静默停写。
// 参数是可变长的，为的是不动既有调用点的签名（那些点已经在 cmd 与测试里）。
func (l *Lease) KeepAlive(every time.Duration, onLost func(error), onTransient ...func(error)) (stop func()) {
	if l == nil {
		return func() {}
	}
	if every <= 0 {
		every = l.ttl / 4
	}
	if every <= 0 {
		every = 30 * time.Second
	}
	var notify func(error)
	if len(onTransient) > 0 {
		notify = onTransient[0]
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				err := l.Renew()
				if err == nil {
					continue
				}
				if errors.Is(err, ErrLost) {
					if onLost != nil {
						onLost(err)
					}
					return
				}
				// 心跳没写成 ≠ 丢了租约：接着续，绝不停写。
				if notify != nil {
					notify(err)
				}
			}
		}
	}()
	return func() {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
		<-doneCh
	}
}

// Release 交还租约。
//
// 只在文件里还是自己那条时才删。已经不是自己的时候**明确报 ErrLost**，
// 而不是静默成功 —— 静默会让调用方以为「我干净退出了」，可实际上
// 它刚才可能一直在和真正的持有者同时写。
//
// 文件不存在时返回 nil：说明已经交还过了（退出路径可能重复调用）。
func (l *Lease) Release() error {
	if l == nil {
		return nil
	}
	current, ok := Holder(l.path)
	if !ok {
		if _, err := os.Stat(l.path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// 文件还在但读不出持有者：没法证明是我的，不去动它。
		return fmt.Errorf("%w：租约文件还在但读不出持有者，本进程没删它", ErrLost)
	}
	if current.Token != l.info.Token {
		return fmt.Errorf("%w：现在是 pid %d（%s）持有，本进程没删它", ErrLost, current.PID, describeNote(current.Note))
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Holder 读出当前租约内容；文件不存在或内容不完整时返回 false。
//
// 读走 fsutil.ReadFile 而不是 os.ReadFile：Windows 上文件正被 rename 覆盖的那一瞬，
// 打开它也会失败。如果这里把读失败当成「没有持有者」，合法持有者续约时就会被
// 判成 ErrLost 而停写 —— 一次瞬时的文件占用不该造成停摆。
func Holder(path string) (Info, bool) {
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		return Info{}, false
	}
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return Info{}, false
	}
	if info.Token == "" {
		return Info{}, false
	}
	return info, true
}

// writeInfoTo 把租约内容写进一个刚被独占创建的文件句柄。
func writeInfoTo(file *os.File, info Info) error {
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// writeInfo 原子覆盖写租约文件（续约用）。
func writeInfo(path string, info Info) error {
	raw, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	// 临时文件名由 fsutil 统一生成：同 pid 的多个 goroutine 不会撞名，
	// 否则撞名会把对方的临时文件截断成 0 字节、再抢一次改名。
	return fsutil.AtomicWriteFile(path, append(raw, '\n'), 0o600)
}

// describeNote 把 note 渲染成可读的一段，没写时给个占位。
func describeNote(note string) string {
	if note == "" {
		return "未注明"
	}
	return note
}

// randomToken 生成持有者标识。
//
// 用时间 + pid + 随机字节拼。它不是安全凭据，只用来回答
// 「文件里这条是不是我刚写的那条」，所以不需要密码学强度。
func randomToken() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%d-%x", time.Now().UnixNano(), os.Getpid(), buf), nil
}
