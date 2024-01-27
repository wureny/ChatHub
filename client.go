package main

//go:generate msgp

import (
	"github.com/gorilla/websocket"
	"log"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Msg struct {
	//发送者地址
	addr string `msg:"addr"`
	//发送时间
	time string `msg:"time"`
	//消息内容
	content string `msg:"content"`
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	//消息格式为：发送者用户名+发送时间+消息内容
	//对消息长度应该有限制
	send chan []byte
}

// readPump pumps messages from the websocket connection to the hub.
// 发送的消息格式为：发送者地址+发送时间+消息内容，这三个都是string类型，最终合并为一个[]string
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	//设置读取消息的最大长度
	c.conn.SetReadLimit(maxMessageSize)
	//设置读取消息的最大时间
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	//设置读取消息的类型
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	//读取消息,将一个Msg对象通过msgp解码为[]byte
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		//新建一个Msg对象，addr为发送者地址，time为发送时间，content为message,另外将time这个time.Time类型转换为string类型
		msg := Msg{addr: c.conn.RemoteAddr().String(), time: time.Now().String(), content: string(message)}
		s, err := msg.MarshalMsg([]byte{})
		c.hub.broadcast <- s
	}
}

func (c *Client) writePump() {}
