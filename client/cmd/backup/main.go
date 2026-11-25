package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"client/internal/backup"
	"client/internal/errs"
)

const usage = `用法:
  backup pack [-algo none|lz77|huffman] [-password 口令] [-progress=false] <源目录> <归档>
  backup unpack [-password 口令] [-progress=false] <归档> <目标目录>
  backup verify [-password 口令] [-progress=false] <归档>

-algo 默认 lz77，压完不比原样小就改存原样
-password 不为空时加密归档，还原和校验要提供同一个口令
-progress 默认开启，把进度打到标准错误输出`

var algorithms = map[string]backup.Algorithm{
	"none":    backup.AlgorithmNone,
	"lz77":    backup.AlgorithmLZ77,
	"huffman": backup.AlgorithmHuffman,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "已取消")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitCode(err))
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageErr("缺少子命令")
	}
	switch args[0] {
	case "pack":
		return runPack(ctx, args[1:])
	case "unpack":
		return runUnpack(ctx, args[1:])
	case "verify":
		return runVerify(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	default:
		return usageErr("未知子命令 %q", args[0])
	}
}

func usageErr(format string, args ...any) error {
	return fmt.Errorf("%s\n%s", fmt.Sprintf(format, args...), usage)
}

func runPack(ctx context.Context, args []string) error {
	c := newCommonFlags("pack")
	algoName := c.fs.String("algo", "lz77", "压缩算法：none、lz77、huffman")
	if err := c.parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if c.fs.NArg() != 2 {
		return usageErr("pack 需要两个参数：<源目录> <归档>")
	}
	algo, ok := algorithms[*algoName]
	if !ok {
		return usageErr("未知算法 %q", *algoName)
	}
	src, dst := c.fs.Arg(0), c.fs.Arg(1)

	entries, err := backup.Scan(src)
	if err != nil {
		return err
	}
	opts := backup.PackOptions{Algorithm: algo, Password: c.password, Progress: progressPrinter(c.progress)}
	skips, err := backup.Pack(ctx, dst, src, entries, opts)
	if err != nil {
		return err
	}
	for _, s := range skips {
		fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", s.Path, s.Err)
	}
	fmt.Printf("已打包 %d 个节点到 %s（算法 %s），跳过 %d 个\n", len(entries)-len(skips), dst, *algoName, len(skips))
	if c.password != "" {
		fmt.Println("归档已加密")
	}
	return nil
}

func runUnpack(ctx context.Context, args []string) error {
	c := newCommonFlags("unpack")
	if err := c.parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if c.fs.NArg() != 2 {
		return usageErr("unpack 需要两个参数：<归档> <目标目录>")
	}
	opts := backup.UnpackOptions{Progress: progressPrinter(c.progress)}
	if err := backup.Unpack(ctx, c.fs.Arg(0), c.fs.Arg(1), c.password, opts); err != nil {
		return err
	}
	fmt.Printf("已还原到 %s\n", c.fs.Arg(1))
	return nil
}

func runVerify(ctx context.Context, args []string) error {
	c := newCommonFlags("verify")
	if err := c.parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if c.fs.NArg() != 1 {
		return usageErr("verify 需要一个参数：<归档>")
	}
	opts := backup.VerifyOptions{Progress: progressPrinter(c.progress)}
	if err := backup.Verify(ctx, c.fs.Arg(0), c.password, opts); err != nil {
		return err
	}
	fmt.Printf("%s 校验通过\n", c.fs.Arg(0))
	return nil
}

// commonFlags 是三个子命令共用的选项
type commonFlags struct {
	fs       *flag.FlagSet
	password string
	progress bool
}

func newCommonFlags(name string) *commonFlags {
	c := &commonFlags{fs: flag.NewFlagSet(name, flag.ContinueOnError)}
	c.fs.SetOutput(io.Discard) // 错误和用法由 usageErr 一起打印
	c.fs.StringVar(&c.password, "password", "", "口令，加密归档要提供")
	c.fs.BoolVar(&c.progress, "progress", true, "把进度打到标准错误输出")
	return c
}

// parse 解析选项；-h 已经按用法打印过
func (c *commonFlags) parse(args []string) error {
	if err := c.fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return flag.ErrHelp
		}
		return usageErr("%v", err)
	}
	return nil
}

// progressPrinter 把进度刷到标准错误输出，同一个阶段原地覆盖
func progressPrinter(enabled bool) func(backup.Progress) {
	if !enabled {
		return nil
	}
	var lastAt time.Time
	return func(p backup.Progress) {
		if p.TotalFiles == 0 {
			fmt.Fprintf(os.Stderr, "%s…\n", p.Phase)
			return
		}
		if p.DoneFiles < p.TotalFiles && time.Since(lastAt) < 100*time.Millisecond {
			return
		}
		lastAt = time.Now()
		fmt.Fprintf(os.Stderr, "\r%s %s %d/%d 个文件 %.1f/%.1f MB",
			p.Phase, p.Path, p.DoneFiles, p.TotalFiles, megabytes(p.DoneBytes), megabytes(p.TotalBytes))
		if p.DoneFiles == p.TotalFiles {
			fmt.Fprintln(os.Stderr)
		}
	}
}

// megabytes 把字节数换成 MB，进度显示用
func megabytes(n int64) float64 { return float64(n) / (1 << 20) }

func exitCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return 130
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return int(e.Code)
	}
	return 1
}
