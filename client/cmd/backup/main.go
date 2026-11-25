package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"client/internal/backup"
	"client/internal/errs"
)

const usage = `用法:
  backup pack [-algo none|lz77|huffman] [-password 口令] <源目录> <归档>
  backup unpack [-password 口令] <归档> <目标目录>
  backup verify [-password 口令] <归档>

-algo 默认 lz77，压完不比原样小就改存原样
-password 不为空时加密归档，还原和校验要提供同一个口令`

var algorithms = map[string]backup.Algorithm{
	"none":    backup.AlgorithmNone,
	"lz77":    backup.AlgorithmLZ77,
	"huffman": backup.AlgorithmHuffman,
}

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
	flags := flag.NewFlagSet("pack", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // 错误和用法由 usageErr 一起打印
	algoName := flags.String("algo", "lz77", "压缩算法：none、lz77、huffman")
	password := flags.String("password", "", "口令，不为空时加密归档")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return usageErr("%v", err)
	}
	if flags.NArg() != 2 {
		return usageErr("pack 需要两个参数：<源目录> <归档>")
	}
	algo, ok := algorithms[*algoName]
	if !ok {
		return usageErr("未知算法 %q", *algoName)
	}
	src, dst := flags.Arg(0), flags.Arg(1)

	entries, err := backup.Scan(src)
	if err != nil {
		return err
	}
	opts := backup.PackOptions{Algorithm: algo, Password: *password}
	skips, err := backup.Pack(ctx, dst, src, entries, opts)
	if err != nil {
		return err
	}
	for _, s := range skips {
		fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", s.Path, s.Err)
	}
	fmt.Printf("已打包 %d 个节点到 %s（算法 %s），跳过 %d 个\n", len(entries)-len(skips), dst, *algoName, len(skips))
	if *password != "" {
		fmt.Println("归档已加密")
	}
	return nil
}

func runUnpack(args []string) error {
	flags, password, err := passwordFlags("unpack", args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return usageErr("unpack 需要两个参数：<归档> <目标目录>")
	}
	if err := backup.Unpack(flags.Arg(0), flags.Arg(1), *password); err != nil {
		return err
	}
	fmt.Printf("已还原到 %s\n", flags.Arg(1))
	return nil
}

func runVerify(args []string) error {
	flags, password, err := passwordFlags("verify", args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return usageErr("verify 需要一个参数：<归档>")
	}
	if err := backup.Verify(flags.Arg(0), *password); err != nil {
		return err
	}
	fmt.Printf("%s 校验通过\n", flags.Arg(0))
	return nil
}

// passwordFlags 解析 unpack、verify 共用的 -password；-h 已经按用法打印过
func passwordFlags(name string, args []string) (*flag.FlagSet, *string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	password := flags.String("password", "", "口令，加密归档要提供")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil, nil, flag.ErrHelp
		}
		return nil, nil, usageErr("%v", err)
	}
	return flags, password, nil
}

func exitCode(err error) int {
	var e *errs.Error
	if errors.As(err, &e) {
		return int(e.Code)
	}
	return 1
}
