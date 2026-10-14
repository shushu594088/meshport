package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"meshport/internal/meshport"
	"meshport/internal/ui"
	"meshport/internal/update"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "0.1.1"

func main() {
	flags := flag.NewFlagSet("meshport", flag.ExitOnError)
	dir := flags.String("data-dir", "/var/lib/meshport", "persistent state directory")
	defaultHost := os.Getenv("MESHPORT_HOST")
	if defaultHost == "" {
		defaultHost = "0.0.0.0"
	}
	host := flags.String("host", defaultHost, "web bind address")
	port := flags.Int("port", 0, "initial web port (only before first configuration)")
	binary := flags.String("mihomo", "/usr/bin/meshport-mihomo", "Mihomo executable")
	helper := flags.String("helper", "/usr/libexec/meshport-helper", "privileged helper")
	controller := flags.Int("controller-port", 9090, "loopback controller port")
	driver := flags.String("driver", "systemd", "systemd or process (development only)")
	secure := flags.Bool("secure-cookie", os.Getenv("MESHPORT_SECURE_COOKIE") == "true", "set Secure session cookies behind HTTPS reverse proxy")
	command := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	_ = flags.Parse(args)
	if command == "version" {
		fmt.Println(version)
		return
	}
	if command == "check-port" {
		if *port < 1024 || *port > 65535 {
			log.Fatal("invalid port")
		}
		listener, e := net.Listen("tcp", net.JoinHostPort(*host, fmt.Sprint(*port)))
		if e != nil {
			log.Fatal("web port unavailable")
		}
		listener.Close()
		return
	}
	if command == "update" && os.Geteuid() != 0 {
		log.Fatal("请使用 sudo meshport update 更新（仅支持 Linux systemd 安装）")
	}
	store, e := meshport.OpenStore(*dir)
	if e != nil {
		log.Fatal("cannot open state: ", e)
	}
	defer store.Close()
	if command == "update" {
		st, err := store.Load()
		if err != nil {
			log.Fatal("无法读取当前配置")
		}
		if st.Pending != nil {
			log.Fatal("TUN 等待确认，请先确认或回退后再更新")
		}
		store.Close()
		fmt.Println("正在检查并更新 Meshport；配置、订阅与代理内核将保留。")
		if err = update.Run(version, st.Settings.WebPort); err != nil {
			log.Fatal(err)
		}
		fmt.Println("版本检查与更新完成。")
		return
	}
	var service meshport.Service = &meshport.SystemService{Helper: *helper}
	if *driver == "process" {
		service = &meshport.ProcessService{Binary: *binary, Dir: *dir}
	} else if *driver != "systemd" {
		log.Fatal("unknown driver")
	}
	app := &meshport.App{Store: store, Kernel: meshport.NewMihomo(*binary, *dir, *controller), Service: service, ControllerPort: *controller}
	switch command {
	case "init":
		fmt.Printf("初始化凭证文件：%s/init-token\n", *dir)
		return
	case "configure":
		unlock, e := store.Lock()
		if e != nil {
			log.Fatal(e)
		}
		defer unlock()
		st, e := store.Load()
		if e != nil {
			log.Fatal(e)
		}
		if st.Revision == 1 && *port != 0 {
			if *port < 1024 || *port > 65535 || *port == 7890 || *port == 7891 || *port == *controller || *port == 1053 {
				log.Fatal("invalid initial port")
			}
			st.Settings.WebPort = *port
			if e = store.Save(st); e != nil {
				log.Fatal(e)
			}
		}
		fmt.Println(st.Settings.WebPort)
		return
	case "autostart":
		st, e := store.Load()
		if e != nil {
			log.Fatal(e)
		}
		fmt.Println(st.Settings.Autostart)
		return
	case "guard":
		if e = app.Guard(); e != nil {
			log.Fatal(e)
		}
		return
	case "rollback":
		if e = app.RollbackDue(true); e != nil {
			log.Fatal(e)
		}
		return
	case "serve":
	default:
		log.Fatal("unknown command")
	}
	if e = app.Prepare(); e != nil {
		log.Fatal("recovery failed: ", e)
	}
	st, e := store.Load()
	if e != nil {
		log.Fatal(e)
	}
	if *port != 0 && st.Revision == 1 {
		st.Settings.WebPort = *port
		unlock, e := store.Lock()
		if e != nil {
			log.Fatal(e)
		}
		e = store.Save(st)
		unlock()
		if e != nil {
			log.Fatal(e)
		}
	}
	st, e = store.Load()
	if e != nil {
		log.Fatal(e)
	}
	handler := &meshport.HTTP{App: app, Host: *host, SecureCookie: *secure, Assets: ui.Handler(), Version: version}
	if e = handler.Listen(st.Settings.WebPort); e != nil {
		log.Fatal(e)
	}
	log.Printf("Meshport %s listening on %s:%d", version, *host, st.Settings.WebPort)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			handler.Shutdown(shutdown)
			cancel()
			return
		case <-ticker.C:
			if e = app.RollbackDue(false); e != nil {
				log.Print("TUN recovery failed; retrying")
			}
		}
	}
}
