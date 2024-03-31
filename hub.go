package main

import (
	"context"
	"golang.org/x/time/rate"
	"sync"
	"time"
)

type clientinfo struct {
	Client       *Client
	Lastmesgsent time.Time
}

type Hub struct {
	clientMutex sync.Mutex
	clients     map[*Client]bool
	// 广播消息缓冲区大小为256
	//另外设置定时器，每隔一段时间清空缓冲区
	//或者超出时清空缓冲区
	broadcast chan []byte

	bufferMutex sync.Mutex
	buffer      chan Msg
	register    chan *Client
	unregister  chan *Client
	//TODO: 该数据结构并不高效
	//TODO: 使用sync.map改造
	allclients   map[string]clientinfo
	clientsMutex sync.Mutex
}

func newHub(bufferofbroadcast int64, maxconnections int64) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		buffer:     make(chan Msg, bufferofbroadcast),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		allclients: make(map[string]clientinfo, maxconnections),
	}
}

func (h *Hub) run() {
	limiter := rate.NewLimiter(350, 350*3)
	ctx := context.Background()
	//h.clientsMutex.Lock()
	//TODO:锁的增加有问题
	/*
		defer h.clientsMutex.Unlock()
		h.bufferMutex.Lock()
		defer h.bufferMutex.Unlock()
		h.clientMutex.Lock()
		defer h.clientMutex.Unlock()
	*/
	//轮询检查不活跃的连接
	go func() {
		for {
			h.checkClientHeartbeat()
			time.Sleep(pongWait)
		}
	}()

	//检查每个用户的lastmsgsent是否在15分钟之内，否则断开连接
	go func() {
		for {
			for i, v := range h.allclients {
				if time.Now().Sub(v.Lastmesgsent) > 15*time.Minute {
					for client := range h.clients {
						if client.conn.RemoteAddr().String() == i {
							close(client.send)
							delete(h.clients, client)
							break
						}
					}
					delete(h.allclients, i)
				}
			}
			time.Sleep(3 * time.Second)
		}
	}()
	//每隔两秒钟将buffer中的消息发送给所有的clients
	//此处还未清空buffer
	go func() {
		var wg sync.WaitGroup
		for client := range h.clients {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case msg := <-h.buffer:
						m, err := msg.MarshalMsg([]byte{})
						if err != nil {
							break
						}
						client.send <- m
					default:
						continue
					}
				}
			}()
		}
		wg.Wait()
	}()
	//处理客户端传来的各种请求
	for {
		select {
		case client := <-h.register:
			if h.clients[client] == true {
				break
			}
			h.clients[client] = true
			addr := client.conn.RemoteAddr().String()
			info, ok := h.allclients[addr]
			if !ok {
				info = clientinfo{
					Client:       client,
					Lastmesgsent: time.Now(),
				}
				h.allclients[addr] = info
			} else {
				info.Lastmesgsent = time.Now()
				h.allclients[addr] = info
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				delete(h.allclients, client.conn.RemoteAddr().String())
				close(client.send)
			}
		case message := <-h.broadcast:
			msg := Msg{addr: "", time: time.Now().String(), content: string(message)}
			_, err := (&msg).UnmarshalMsg(message)
			if err != nil {
				break
			}
			addr := msg.addr
			info, ok := h.allclients[addr]
			if ok {
				info.Lastmesgsent = time.Now()
				h.allclients[addr] = info
			}
			e := limiter.Wait(ctx)
			if e != nil {
				time.Sleep(1 * time.Second)
			}
			if e != nil {
				break
			}
			h.buffer <- msg
		}
	}
}

func (h *Hub) checkClientHeartbeat() {
	// 使用互斥锁确保并发安全
	h.clientMutex.Lock()
	defer h.clientMutex.Unlock()

	// 遍历客户端，发送 Ping 消息
	for client := range h.clients {
		select {
		case client.send <- []byte("ping"):
			// Ping 消息成功发送
		default:
			// 发送失败，可能是通道已关闭，需要处理失活的客户端
			close(client.send)
			delete(h.clients, client)
		}
	}
}
