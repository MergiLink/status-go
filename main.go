package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
	"runtime"

	"github.com/gorilla/websocket"
	"github.com/go-ping/ping"
)

const (
	webSocketURL       = "ws://"
	reconnectInterval  = 5 * time.Second
	serverInfoEndpoint = "http://127.0.0.1:37549/api/serverinfo"
	authUsername       = "admin"
	authPassword       = "SQML"
	timeoutDuration    = 6 * time.Second
)

type Message struct {
	Type   string `json:"type"`
	Sid    string `json:"sid"`
	Action string `json:"action"`
	IP     string `json:"ip,omitempty"`
	UID    string `json:"uid,omitempty"`
	Code   int    `json:"code,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

func connectWebSocket(sid int) {
	for {
		c, _, err := websocket.DefaultDialer.Dial(webSocketURL, nil)
		if err != nil {
			fmt.Printf("%s WebSocket 连接失败: %v\n", time.Now().Format(time.RFC3339), err)
			time.Sleep(reconnectInterval)
			continue
		}
		defer c.Close()

		fmt.Printf("%s 已连接到 WebSocket 服务器\n", time.Now().Format(time.RFC3339))

		// 将 sid 转换为 string
		sidStr := strconv.Itoa(sid)

		// 发送初始化消息
		c.WriteJSON(Message{Type: "node", Sid: sidStr})

		// 定时发送心跳
		go func() {
			for {
				c.WriteJSON(Message{Type: "node", Code: 200, Sid: sidStr, Action: "head"})
				time.Sleep(30 * time.Second)
			}
		}()

		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				fmt.Printf("%s WebSocket 连接断开: %v\n", time.Now().Format(time.RFC3339), err)
				break
			}

			var data Message
			if err := json.Unmarshal(message, &data); err != nil {
				fmt.Printf("%s 无法解析消息: %v\n", time.Now().Format(time.RFC3339), err)
				continue
			}

			switch data.Action {
			case "ping":
				go handlePing(c, data, sidStr)
			case "info":
				go handleInfo(c, data, sidStr)
			}
		}
	}
}

func handlePing(c *websocket.Conn, data Message, sid string) {
	fmt.Printf("%s 正在 Ping %s 用户: %s\n", time.Now().Format(time.RFC3339), data.IP, data.UID)

	pinger, err := ping.NewPinger(data.IP)

	// 根据操作系统设置 Privileged 模式
	if runtime.GOOS == "windows" {
		pinger.SetPrivileged(true)
	}
	
	pinger.Size = 548

	if err != nil {
		fmt.Printf("%s Ping 初始化失败: %v\n", time.Now().Format(time.RFC3339), err)
		c.WriteJSON(Message{
			Type:   "node",
			Sid:    sid,
			Code:   500,
			UID:    data.UID,
			Action: "ping",
			Result: json.RawMessage([]byte(fmt.Sprintf(`{"error": "%s"}`, err.Error()))), // 转换错误信息为 JSON
		})
		return
	}

	pinger.Count = 3
	err = pinger.Run()
	if err != nil {
		fmt.Printf("%s Ping 失败: %v\n", time.Now().Format(time.RFC3339), err)
		c.WriteJSON(Message{
			Type:   "node",
			Sid:    sid,
			Code:   500,
			UID:    data.UID,
			Action: "ping",
			Result: json.RawMessage([]byte(fmt.Sprintf(`{"error": "%s"}`, err.Error()))), // 转换错误信息为 JSON
		})
		return
	}

stats := pinger.Statistics()

// 构造简单的字符串结果
result := fmt.Sprintf("%dms", stats.AvgRtt.Milliseconds())

fmt.Printf("%s 用户 %s - %s Ping: %s\n", time.Now().Format(time.RFC3339), data.UID, data.IP, result)

c.WriteJSON(Message{
    Type:   "node",
    Sid:    sid,
    Code:   200,
    UID:    data.UID,
    Action: "ping",
    Result: json.RawMessage([]byte(fmt.Sprintf(`"%s"`, result))), // 包装为 JSON 字符串
})

}

func handleInfo(c *websocket.Conn, data Message, sid string) {
	client := &http.Client{Timeout: timeoutDuration}
	req, err := http.NewRequest("GET", serverInfoEndpoint, nil)
	if err != nil {
		fmt.Printf("%s 构建请求失败: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}

	auth := base64.StdEncoding.EncodeToString([]byte(authUsername + ":" + authPassword))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("%s 请求服务器状态失败: %v\n", time.Now().Format(time.RFC3339), err)
		c.WriteJSON(Message{
			Type:   "node",
			Sid:    sid,
			Code:   500,
			UID:    data.UID,
			Action: "info",
			Result: json.RawMessage([]byte(fmt.Sprintf(`{"error": "%s"}`, err.Error()))), // 转换错误信息为 JSON
		})
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("%s 读取响应失败: %v\n", time.Now().Format(time.RFC3339), err)
		return
	}

	fmt.Printf("%s 获取服务器状态成功 用户: %s\n", time.Now().Format(time.RFC3339), data.UID)
	c.WriteJSON(Message{
		Type:   "node",
		Sid:    sid,
		Code:   200,
		UID:    data.UID,
		Action: "info",
		Result: json.RawMessage(body), // 直接传递响应的 JSON 数据
	})
}

func main() {
	var sid int
	flag.IntVar(&sid, "sid", 0, "指定 SID 值")
	flag.Parse()

	if sid == 0 {
		fmt.Println("SID 必须指定，例如 --sid=7")
		return
	}

	connectWebSocket(sid)
}
