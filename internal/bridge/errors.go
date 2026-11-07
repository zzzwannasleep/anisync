package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/lease"
)

// 错误码。取值由 docs/ops-api.md §3 固定，Dart 侧按它们分支，不许改一个字。
const (
	CodeBadRequest    = "bad_request"
	CodeNotFound      = "not_found"
	CodeInvalidConfig = "invalid_config"
	CodeNetwork       = "network"
	CodeBusy          = "busy"
	CodeLeaseHeld     = "lease_held"
	CodeCanceled      = "canceled"
	CodeInternal      = "internal"
)

// ErrorPayload 是信封里的错误体。
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// Error 是本层的领域错误：它已经知道自己是哪个契约错误码。
type Error struct {
	Code    string
	Message string
	Detail  string
	Err     error
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Message + "（" + e.Detail + "）"
	}
	return e.Message
}

// Unwrap 让 errors.Is / errors.As 能继续往下钻。
func (e *Error) Unwrap() error { return e.Err }

// NewError 造一个带错误码的错误。
func NewError(code, message, detail string) *Error {
	return &Error{Code: code, Message: message, Detail: detail}
}

// Errorf 是 NewError 的格式化快捷键。
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// BadRequest 表示 payload 字段缺失或非法。
func BadRequest(format string, args ...any) *Error {
	return Errorf(CodeBadRequest, format, args...)
}

// NotFound 表示实例名 / 方案名 / 文件 / 任务不存在。
func NotFound(format string, args ...any) *Error {
	return Errorf(CodeNotFound, format, args...)
}

// NotFoundNamed 造一个「**具名资源**不存在」的 not_found：detail **逐字**就是那个资源名。
//
// `not_found` 有两种含义，客户端靠 `detail` 区分（契约 §3）：
//
//	detail == 那个 op 名  -> 「这个 op 不认识」（客户端内核比节点新 → 版本偏斜）
//	detail == 资源名      -> 「op 认识，但它要的那个东西没有」
//
// 所以两条要求：op 分支的 detail 逐字等于 op 名（见 registry.go 的未知 op 分支），
// 资源分支的 detail 逐字等于资源名。**不要拼成 `job=zzz` 这种复合串**——
// 判别规则是拿 detail 与 op 名对等的，一拼接两边都失效。
//
// 为什么值得为它单独写一个构造函数、而不再去解析 message：
// message 是会被翻译和改写的人话，契约明文禁止客户端解析它；
// 而 UI 需要把「哪个资源没有」显示出来（例如「任务 zzz 找不到」）。
func NotFoundNamed(name string, format string, args ...any) *Error {
	e := Errorf(CodeNotFound, format, args...)
	e.Detail = name
	return e
}

// withResourceDetail 给一个已经判成 not_found 的错误补上资源名。
//
// 用在「错误由下游产生、而名字只有上游知道」的路径上：例如
// `app.FindInstance` 只会说「找不到实例」，具体是哪个名字得由调用方补。
//
// 三条克制：**只在 not_found 上补**、**不覆盖已有的 detail**、
// **名字为空时不补**。detail 是判别用的，被覆盖就等于判别失效。
func withResourceDetail(err error, name string) *Error {
	e := Classify(err)
	if e == nil || e.Code != CodeNotFound || e.Detail != "" || name == "" {
		return e
	}
	e.Detail = name
	return e
}

// InvalidConfig 表示配置被 config.Validate 拦下了。
func InvalidConfig(err error) *Error {
	return &Error{Code: CodeInvalidConfig, Message: err.Error(), Err: err}
}

// Network 表示连不上实例或上游。
func Network(err error) *Error {
	return &Error{Code: CodeNetwork, Message: err.Error(), Err: err}
}

// Busy 表示已经有一个写操作在飞。
func Busy(format string, args ...any) *Error {
	return Errorf(CodeBusy, format, args...)
}

// Canceled 表示调用被 AniSyncCancel 取消。
func Canceled(err error) *Error {
	if err == nil {
		err = context.Canceled
	}
	return &Error{Code: CodeCanceled, Message: "调用已取消", Err: err}
}

// Internal 表示其它错误。
func Internal(err error) *Error {
	return &Error{Code: CodeInternal, Message: err.Error(), Err: err}
}

// Classify 把领域错误映射成契约错误码。
//
// 这是唯一一处做映射的地方：op 内部只负责「把错抛出来」，
// 至于它是网络问题还是配置问题，由这里按类型判定 ——
// 每个 op 自己写一份 if/else 的话，「填错 apiKey」在 A op 报 network、
// 在 B op 报 internal，界面上的提示就会自相矛盾。
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var already *Error
	if errors.As(err, &already) {
		return already
	}
	switch {
	case errors.Is(err, context.Canceled):
		return Canceled(err)
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: CodeNetwork, Message: err.Error(), Err: err}
	case errors.Is(err, lease.ErrHeld), errors.Is(err, lease.ErrLost):
		return &Error{Code: CodeLeaseHeld, Message: err.Error(), Err: err}
	case errors.Is(err, app.ErrInstanceNotFound), errors.Is(err, os.ErrNotExist):
		return &Error{Code: CodeNotFound, Message: err.Error(), Err: err}
	case isNetwork(err):
		return Network(err)
	}
	return Internal(err)
}

// isNetwork 判断一个错误是不是「连不上 / 读不动」这一类。
func isNetwork(err error) bool {
	var apiErr *assapi.APIError
	if errors.As(err, &apiErr) {
		return true
	}
	var bgmErr *bgm.Error
	if errors.As(err, &bgmErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// 代理/连接层的裸错误文本：这些错误没有类型可判，
	// 但它们在界面上确实属于「可重试」，归到 internal 会让人以为程序坏了。
	text := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused", "connection reset", "no such host",
		"i/o timeout", "tls handshake", "proxyconnect", "dial tcp", "eof",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
