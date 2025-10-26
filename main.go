package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	pb "learn-master-slave/proto/learn-master-slave/taskpb"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
)

// TaskStatus represents the task lifecycle maintained by master
type TaskStatus string

const (
	Pending TaskStatus = "PENDING"
	Running TaskStatus = "RUNNING"
	Success TaskStatus = "SUCCESS"
	Failed  TaskStatus = "FAILED"
	Timeout TaskStatus = "TIMEOUT"
)

type TaskEntry struct {
	Task     *pb.Task
	Status   TaskStatus
	Assigned string // worker id
	Start    time.Time
	Attempts int32
	cancelFn context.CancelFunc
}

// WorkerConn wraps a connected worker stream
type WorkerConn struct {
	id     string
	msgs   chan *pb.MasterMsg
	ctx    context.Context
	cancel context.CancelFunc
}

type MasterServer struct {
	pb.UnimplementedMasterServer

	mu      sync.Mutex
	workers map[string]*WorkerConn
	// 执行中的任务数组，超时错误等都会将任务移除
	tasks map[string]*TaskEntry
	// 任务队列
	taskQueue chan *pb.Task
}

func NewMaster() *MasterServer {
	return &MasterServer{
		workers: make(map[string]*WorkerConn),
		tasks:   make(map[string]*TaskEntry),
		// 总的任务队列
		taskQueue: make(chan *pb.Task, 1024),
	}
}

// WorkerStream handles bidirectional stream with worker
func (m *MasterServer) WorkerStream(stream pb.Master_WorkerStreamServer) error {
	// First, receive hello (worker registers)
	// We will also concurrently send MasterMsg via stream.Send

	// Create channels
	recv := make(chan *pb.WorkerMsg)
	done := make(chan error, 1)

	// reader goroutine
	go func() {
		for {
			// 收到消息
			wm, err := stream.Recv()
			if err != nil {
				done <- err
				return
			}
			recv <- wm
		}
	}()

	var workerID string
	// waiting for hello
	select {
	case wm := <-recv:
		if h := wm.GetHello(); h != nil {
			workerID = h.WorkerId
		} else {
			return errors.New("expected hello first")
		}
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("timeout waiting for hello")
	}

	// register worker
	ctx, cancel := context.WithCancel(context.Background())
	wc := &WorkerConn{id: workerID, msgs: make(chan *pb.MasterMsg, 128), ctx: ctx, cancel: cancel}

	m.mu.Lock()
	m.workers[workerID] = wc
	m.mu.Unlock()

	log.Printf("worker %s connected", workerID)

	// sender goroutine
	senderErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-wc.ctx.Done():
				senderErr <- wc.ctx.Err()
				return
				// 有任务，直接发送到客户端
			case mm := <-wc.msgs:
				if err := stream.Send(mm); err != nil {
					senderErr <- err
					return
				}
			}
		}
	}()

	// main loop: handle incoming worker messages and lifecycle
	for {
		select {
		// 收到客户端的回复
		case wm := <-recv:
			if r := wm.GetResult(); r != nil {
				m.handleResult(r)
			}
		case err := <-senderErr:
			log.Printf("sender error for worker %s: %v", workerID, err)
			m.unregisterWorker(workerID)
			return err
		case err := <-done:
			log.Printf("recv error for worker %s: %v", workerID, err)
			m.unregisterWorker(workerID)
			return err
		}
	}
}

func (m *MasterServer) unregisterWorker(id string) {
	m.mu.Lock()
	if wc, ok := m.workers[id]; ok {
		wc.cancel()
		close(wc.msgs)
		delete(m.workers, id)
	}
	m.mu.Unlock()
}

func (m *MasterServer) handleResult(r *pb.TaskResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	te, ok := m.tasks[r.Id]
	if !ok {
		log.Printf("received result for unknown task %s", r.Id)
		return
	}
	if r.Success {
		// 任务执行成功
		te.Status = Success
		if te.cancelFn != nil {
			// 取消超时检测
			te.cancelFn() // cancel timeout
		}
		log.Printf("task %s succeeded", r.Id)
		// cleanup
		delete(m.tasks, r.Id)
	} else {
		te.Status = Failed
		if te.cancelFn != nil {
			te.cancelFn()
		}
		log.Printf("task %s failed: %s", r.Id, r.Error)
		delete(m.tasks, r.Id)
		// reschedule if attempts < 3
		if te.Attempts < 3 {
			te.Attempts++
			te.Task.Attempts = te.Attempts
			go func(t *pb.Task) { m.taskQueue <- t }(te.Task)
		} else {
			log.Printf("task %s reached max attempts", r.Id)
		}
	}
}

// dispatch loop: take tasks from taskQueue and assign to an available worker
func (m *MasterServer) dispatchLoop() {
	// 所有任务都在这个队列里面
	for t := range m.taskQueue {
		// 寻找worker节点调度
		assigned := m.assignTask(t)
		if !assigned {
			log.Printf("no available worker, requeue task %s", t.Id)
			// simple backoff
			// 调度失败，等待时间再次调度
			time.AfterFunc(time.Second, func() { m.taskQueue <- t })
		}
	}
}

// 选择一个worker节点，发送任务给这个worker节点，可能失败
func (m *MasterServer) assignTask(t *pb.Task) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.workers) == 0 {
		return false
	}
	// pick a random worker for simplicity
	idx := rand.Intn(len(m.workers))
	var chosen *WorkerConn
	i := 0
	for _, w := range m.workers {
		if i == idx {
			chosen = w
			break
		}
		i++
	}
	if chosen == nil {
		return false
	}

	// create task entry and start timeout watcher
	te := &TaskEntry{Task: t, Status: Running, Assigned: chosen.id, Start: time.Now(), Attempts: t.Attempts}
	ctx, cancel := context.WithCancel(context.Background())
	te.cancelFn = cancel
	// 同时记录任务到taskMap，用于追踪状态
	m.tasks[t.Id] = te

	// send task
	mm := &pb.MasterMsg{M: &pb.MasterMsg_Task{Task: t}}
	select {
	// 发送到任务队列成功
	case chosen.msgs <- mm:
		log.Printf("assigned task %s to worker %s", t.Id, chosen.id)
		// 队列满了
	default:
		log.Printf("worker %s msg channel full, cannot assign", chosen.id)
		delete(m.tasks, t.Id)
		return false
	}

	// start timeout watcher goroutine
	go m.waitTaskTimeout(ctx, t.Id, 10*time.Second)

	return true
}

func (m *MasterServer) waitTaskTimeout(ctx context.Context, taskID string, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	// 任务已经执行结束，可能成功或者失败
	case <-ctx.Done():
		return
	case <-timer.C:
		// timeout occurred
		m.mu.Lock()
		te, ok := m.tasks[taskID]
		if !ok {
			m.mu.Unlock()
			return
		}
		// 任务执行超时了，需要重新调度
		// mark timeout, remove and reschedule
		te.Status = Timeout
		log.Printf("task %s timed out on worker %s", taskID, te.Assigned)
		delete(m.tasks, taskID)
		m.mu.Unlock()

		if te.Attempts < 3 {
			te.Attempts++
			te.Task.Attempts = te.Attempts
			// 重新触发调度
			m.taskQueue <- te.Task
		} else {
			log.Printf("task %s reached max attempts after timeout", taskID)
		}
	}
}

func main() {
	port := flag.Int("port", 50051, "master gRPC port")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	master := NewMaster()
	pb.RegisterMasterServer(grpcServer, master)

	// start dispatcher
	go master.dispatchLoop()

	// for demo: populate some tasks periodically
	go func() {
		id := 1
		for {
			// 随机产生任务
			t := &pb.Task{Id: fmt.Sprintf("task-%d", id), Payload: fmt.Sprintf("payload-%d", id), Attempts: 0}
			id++
			master.taskQueue <- t
			time.Sleep(2 * time.Second)
		}
	}()

	log.Printf("master listening on %d", *port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Serve: %v", err)
	}
}
