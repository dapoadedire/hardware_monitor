package main

import (
	"context"
	"fmt"
	"hardware_monitor/internal/hardware"
	"net/http"
	"os"
	"sync"
	"time"
	
	"github.com/coder/websocket"
)

type server struct {
	subscriberMessageBuffer int
	mux                     http.ServeMux
	subscribersMutex        sync.Mutex
	subscribers             map[*subscriber]struct{}
}

type subscriber struct {
	msgs chan []byte
}

func NewServer() *server {
	s := &server{
		subscriberMessageBuffer: 10,
		subscribers:             make(map[*subscriber]struct{}),
	}
	s.mux.Handle("/", http.FileServer(http.Dir("./htmx")))
	s.mux.HandleFunc("/ws", s.subscribeHandler)
	return s
}

func (s *server) addSubscriber(subscriber *subscriber) {
	s.subscribersMutex.Lock()
	s.subscribers[subscriber] = struct{}{}
	s.subscribersMutex.Unlock()
	fmt.Println("Added subscriber,", subscriber)
}

func (s *server) subscribe(ctx context.Context, writer http.ResponseWriter, req *http.Request) error {
	var c *websocket.Conn
	subscriber := &subscriber{
		msgs: make(chan []byte, s.subscriberMessageBuffer),
	}
	s.addSubscriber(subscriber)

	c, err := websocket.Accept(writer, req, nil)
	if err != nil {
		return nil
	}
	defer c.CloseNow()

	ctx = c.CloseRead(ctx)

	for {
		select {
		case msg := <-subscriber.msgs:
			ctx, cancel := context.WithTimeout(
				ctx,
				time.Second,
			)
			defer cancel()
			err := c.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

}

func (s *server) subscribeHandler(writer http.ResponseWriter, req *http.Request) {
	err := s.subscribe(req.Context(), writer, req)
	if err != nil {
		fmt.Println(err)
		return
	}
}

func (s *server) broadcast(msg []byte) {
	s.subscribersMutex.Lock()
	for subscriber := range s.subscribers {
		select {
		case subscriber.msgs <- msg:
		default:
			fmt.Println("Subscriber buffer full, dropping message")
		}
	}

	s.subscribersMutex.Unlock()

}

func main() {
	fmt.Println("Starting system monitor")
	srv := NewServer()
	go func(s *server) {
		for {
			systemSection, err := hardware.GetSystemSection()
			if err != nil {
				fmt.Println(err)
			}
			diskSection, err := hardware.GetDiskSection()
			if err != nil {
				fmt.Println(err)
			}
			cpuSection, err := hardware.GetCpuSection()
			if err != nil {
				fmt.Println(err)
			}

			timeStamp := time.Now().Format("2006-01-02 15:04:05")
			html :=
				`<div hx-swap-oob="innerHTML:#update-timestamp">` + timeStamp + `</div>
			     <div hx-swap-oob="innerHTML:#system-data">` + systemSection + `</div>
			     <div hx-swap-oob="innerHTML:#disk-data">` + diskSection + `</div>
			     <div hx-swap-oob="innerHTML:#cpu-data">` + cpuSection + `</div>`

			s.broadcast([]byte(html))

			time.Sleep(1 * time.Second)

		}
	}(srv)

	err := http.ListenAndServe(":8080", &srv.mux)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
