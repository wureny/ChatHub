package main

import (
	"context"
	"golang.org/x/time/rate"
	"sync"
	"time"
)

type clientinfo struct {
	addr         string
	lastmesgsent time.Time
}

// TODO：将buffer去掉，将broadcast修改为带有缓冲的channel
type Hub struct {
	//TODO：加锁究竟有没有必要
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

	allclients   []clientinfo
	clientsMutex sync.Mutex
}

const CAPOFBUFFER = 100

func newHub(bufferofbroadcast int64, maxconnections int64) *Hub {
	return &Hub{
		clientMutex: sync.Mutex{},
		clients:     make(map[*Client]bool),

		broadcast: make(chan []byte),

		bufferMutex: sync.Mutex{},
		buffer:      make(chan Msg, bufferofbroadcast),

		register:   make(chan *Client),
		unregister: make(chan *Client),

		clientsMutex: sync.Mutex{},
		allclients:   make([]clientinfo, 0, maxconnections),
	}
}

func (h *Hub) run() {
	limiter := rate.NewLimiter(350, 350*3)
	ctx := context.Background()
	h.clientsMutex.Lock()
	defer h.clientsMutex.Unlock()
	h.bufferMutex.Lock()
	defer h.bufferMutex.Unlock()
	h.clientMutex.Lock()
	defer h.clientMutex.Unlock()

	//轮询检查不活跃的连接
	go func() {
		for {
			h.checkClientHeartbeat()
			time.Sleep(pongWait)
		}
	}()
	//检查buffer是否大于CAPOFBUFFER，满了就批量发送消息给clients
	/*	go func() {
			for {
				if len(h.buffer) >= CAPOFBUFFER {
					//将h中的内容mashal为[]byte，批量发送给clients
					for client := range h.clients {
						for _, v := range h.buffer {
							msg, err := v.MarshalMsg([]byte{})
							if err != nil {
								break
							}
							client.send <- msg
						}
					}
				}
				//time.Sleep(3 * time.Second)
			}
		}()
	*/
	//检查每个用户的lastmsgsent是否在15分钟之内，否则断开连接
	go func() {
		for {
			for i, v := range h.allclients {
				if time.Now().Sub(v.lastmesgsent) > 15*time.Minute {
					for client := range h.clients {
						if client.conn.RemoteAddr().String() == v.addr {
							close(client.send)
							delete(h.clients, client)
							break
						}
					}
					h.allclients = append(h.allclients[:i], h.allclients[i+1:]...)
				}
			}
			time.Sleep(3 * time.Second)
		}
	}()
	//每隔两秒钟将buffer中的消息发送给所有的clients
	//此处还未清空buffer
	//TODO：此处不会和上面清空buffer的代码有冲突吗，即有没有可能一个消息被发送两次
	go func() {
		for client := range h.clients {
			/*for _, v := range h.buffer {
				msg, err := v.MarshalMsg([]byte{})
				if err != nil {
					break
				}
				client.send <- msg
			}*/
			go func() {
				for {
					select {
					case msg := <-h.buffer:
						m, err := msg.MarshalMsg([]byte{})
						if err != nil {
							break
						}
						client.send <- m
					default:
						break
					}
				}
				//time.Sleep(2 * time.Second)
			}()
		}
	}()
	//heartbeatTicker := time.NewTicker(15 * time.Second)
	for {
		select {
		//TODO：处理最大连接数
		case client := <-h.register:
			if h.clients[client] == true {
				break
			}
			h.clients[client] = true
			h.allclients = append(h.allclients, clientinfo{client.conn.RemoteAddr().String(), time.Now()})
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				for i, v := range h.allclients {
					if v.addr == client.conn.RemoteAddr().String() {
						h.allclients = append(h.allclients[:i], h.allclients[i+1:]...)
						break
					}
				}
				close(client.send)
			}
		//定时器定时触发websocket ping/pong检测
		/*case <-heartbeatTicker.C:
		for client := range h.clients {
			select {
			case client.send <- []byte("ping"):
			default:
				close(client.send)
				delete(h.clients, client)
			}
		}*/
		//TODO：数据格式问题
		case message := <-h.broadcast:
			msg := Msg{addr: "", time: time.Now().String(), content: string(message)}
			_, err := (&msg).UnmarshalMsg(message)
			if err != nil {
				break
			}
			go func() {
				for i, v := range h.allclients {
					if v.addr == msg.addr {
						h.allclients[i].lastmesgsent = time.Now()
						break
					}
				}
			}()
			//TODO：限流
			e := limiter.Wait(ctx)
			if e != nil {
				time.Sleep(1 * time.Second)
			}
			if e != nil {
				break
			}
			/*
				//检查buffer是否已满，没满便加入buffer缓存
				if len(h.buffer)+1 <= CAPOFBUFFER {
					h.buffer = append(h.buffer, msg)
				} else {
					time.Sleep(3 * time.Second)
				}
				if len(h.buffer)+1 <= CAPOFBUFFER {
					h.buffer = append(h.buffer, msg)
				}
			*/
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
