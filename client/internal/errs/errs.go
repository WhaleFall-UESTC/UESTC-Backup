// Package errs 是客户端统一的错误类别和错误类型。
package errs

import (
	"fmt"
	"strings"
)

// Code 是错误类别。数值会拿去当命令行退出码，定下来就不再改，新的加在最后。
type Code int

// Code 的取值，数值就是命令行的退出码
const (
	NotArchive Code = iota + 1
	UnsupportedVersion
	Corrupted
	SourceChanged
	IO
	InvalidArgument
	TargetNotEmpty
	WrongPassword
	PartialBackup
	JobBusy
)

var phrases = map[Code]string{
	NotArchive:         "not a .dbkp archive",
	UnsupportedVersion: "unsupported archive version",
	Corrupted:          "archive is corrupted",
	SourceChanged:      "source file changed since it was scanned",
	IO:                 "i/o error",
	InvalidArgument:    "invalid argument",
	TargetNotEmpty:     "target directory is not empty",
	WrongPassword:      "wrong password",
	PartialBackup:      "backup skipped files",
	JobBusy:            "another job is running",
}

// String 返回类别的固定说法。
func (c Code) String() string { return phrases[c] }

// 各类别的哨兵错误，用 errors.Is 判断
var (
	ErrNotArchive         = &Error{Code: NotArchive}
	ErrUnsupportedVersion = &Error{Code: UnsupportedVersion}
	ErrCorrupted          = &Error{Code: Corrupted}
	ErrSourceChanged      = &Error{Code: SourceChanged}
	ErrIO                 = &Error{Code: IO}
	ErrInvalidArgument    = &Error{Code: InvalidArgument}
	ErrTargetNotEmpty     = &Error{Code: TargetNotEmpty}
	ErrWrongPassword      = &Error{Code: WrongPassword}
	ErrPartialBackup      = &Error{Code: PartialBackup}
	ErrJobBusy            = &Error{Code: JobBusy}
)

// Error 是引擎自己判定出来的错误。
type Error struct {
	Code   Code
	Path   string
	Detail string
	Err    error
}

// New 造一个只有类别的错误。
func New(code Code) *Error { return Newf(code, "") }

// Newf 造一个带类别的错误，format 和 args 的用法同 fmt.Errorf。
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Wrap 给底层错误加上类别。
func Wrap(err error, code Code) *Error { return Wrapf(err, code, "") }

// Wrapf 同 Wrap，format 和 args 的用法同 fmt.Errorf。
func Wrapf(err error, code Code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...), Err: err}
}

// At 返回一个标明了出错文件的副本，e 自己不变，所以对哨兵调用也安全。
func (e *Error) At(path string) *Error {
	clone := *e
	clone.Path = path
	return &clone
}

// 各部分用 ": " 连接，空的不出现；有底层原因时它排在最后，类别不再出现。
func (e *Error) Error() string {
	var parts []string
	for _, s := range []string{e.Path, e.Detail} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if e.Err == nil {
		parts = append(parts, e.Code.String())
	} else {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

// Unwrap 返回底层原因。
func (e *Error) Unwrap() error { return e.Err }

// Is 按类别匹配，所以 errors.Is(err, ErrCorrupted) 对所有类别为 Corrupted 的错误都成立。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && e.Code == t.Code
}
