package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"client/internal/backup"
	"client/internal/errs"
)

const usage = `用法:
  backup pack <源目录> <归档>
  backup unpack <归档> <目标目录>
  backup verify <归档>`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
		return runUnpack(args[1:])
	case "verify":
		return runVerify(args[1:])
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
	if len(args) != 2 {
		return usageErr("pack 需要两个参数：<源目录> <归档>")
	}
	src, dst := args[0], args[1]

	entries, err := backup.Scan(src)
	if err != nil {
		return err
	}
	skips, err := backup.Pack(ctx, dst, src, entries, nil)
	if err != nil {
		return err
	}
	for _, s := range skips {
		fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", s.Path, s.Err)
	}
	fmt.Printf("已打包 %d 个节点到 %s，跳过 %d 个\n", len(entries)-len(skips), dst, len(skips))
	return nil
}

func runUnpack(args []string) error {
	if len(args) != 2 {
		return usageErr("unpack 需要两个参数：<归档> <目标目录>")
	}
	if err := backup.Unpack(args[0], args[1]); err != nil {
		return err
	}
	fmt.Printf("已还原到 %s\n", args[1])
	return nil
}

func runVerify(args []string) error {
	if len(args) != 1 {
		return usageErr("verify 需要一个参数：<归档>")
	}
	if err := backup.Verify(args[0]); err != nil {
		return err
	}
	fmt.Printf("%s 校验通过\n", args[0])
	return nil
}

func exitCode(err error) int {
	var e *errs.Error
	if errors.As(err, &e) {
		return int(e.Code)
	}
	return 1
}
