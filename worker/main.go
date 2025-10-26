package worker

import (
	"context"
	"flag"
	"google.golang.org/grpc"
	"io"
	pb "learn-master-slave/proto/learn-master-slave/taskpb"
	"log"
	"time"
)

func runWorker(masterAddr, workerID string) error {
	conn, err := grpc.Dial(masterAddr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewMasterClient(conn)

	ctx := context.Background()
	// open stream
	stream, err := client.WorkerStream(ctx)
	if err != nil {
		return err
	}

	// send hello
	if err := stream.Send(&pb.WorkerMsg{M: &pb.WorkerMsg_Hello{Hello: &pb.WorkerHello{WorkerId: workerID}}}); err != nil {
		return err
	}

	// receiver goroutine
	go func() {
		for {
			mm, err := stream.Recv()
			if err == io.EOF {
				log.Println("stream closed by master")
				return
			}
			if err != nil {
				log.Printf("recv error: %v", err)
				return
			}
			if t := mm.GetTask(); t != nil {
				go handleTask(stream, t)
			}
		}
	}()

	// keep alive
	select {}
}

func handleTask(stream pb.Master_WorkerStreamClient, task *pb.Task) {
	log.Printf("worker: got task %s payload=%s attempts=%d", task.Id, task.Payload, task.Attempts)
	// simulate work; fail if payload contains "fail" or random
	// simulate variable duration
	dur := time.Duration(2+task.Attempts) * time.Second
	time.Sleep(dur)

	success := true
	var errStr string
	// random fail for demo
	if task.Attempts%2 == 1 {
		// if attempts odd, simulate failure (demo)
		success = false
		errStr = "simulated-error"
	}

	// send result back
	res := &pb.WorkerMsg{M: &pb.WorkerMsg_Result{Result: &pb.TaskResult{Id: task.Id, Success: success, Error: errStr}}}
	if err := stream.Send(res); err != nil {
		log.Printf("failed to send result: %v", err)
	}
}

func main() {
	master := flag.String("master", "localhost:50051", "master address")
	id := flag.String("id", "worker-1", "worker id")
	flag.Parse()

	log.Printf("worker %s connecting to %s", *id, *master)
	if err := runWorker(*master, *id); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}
