package main

// how to / should we close a websocket connection after the command is completed?

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rakyll/statik/fs"
	_ "viemacs/rcserver/statik"
)

func main() {
	const port int = 8080
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	gin.SetMode(gin.ReleaseMode)

	initCommand()

	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		c.Writer.WriteHeader(http.StatusOK)
		c.Writer.Header().Set("Content-Type", "text/html")
		c.Writer.Write(pageIndex("/index.html"))
	})
	router.GET("/ws", ws)
	router.POST("/rc/*action", callCommand)

	log.Printf("remote command server @ %v", port)
	router.Run(fmt.Sprintf(":%d", port))
}

var commands map[string]string = make(map[string]string)
var wsconns map[string]*websocket.Conn = make(map[string]*websocket.Conn)
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func initCommand() {
	fb, err := ioutil.ReadFile("command.conf")
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range strings.Split(string(fb), "\n") {
		line = strings.Trim(line, " \t")
		pos := strings.Index(line, "=")
		if strings.HasPrefix(line, "#") || pos == -1 || pos >= len(line)-1 {
			continue
		}
		key, value := strings.Trim(line[0:pos], " \t"), strings.Trim(line[pos+1:], " \t")
		commands[key] = value
		log.Printf("command map: %s = %s", key, value)
	}
}

func callCommand(c *gin.Context) {
	action := c.Param("action")
	action = strings.TrimLeft(action, "/")
	command, ok := commands[action]
	if !ok {
		log.Println("command key not found:", action)
		return
	}
	var args []string
	c.BindJSON(&args)

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Header().Set("Content-Type", "text/plain")
	c.Writer.Write([]byte(strings.Join(append([]string{"rc: " + command}, args...), " ")))

	log.Printf("calling command: %s %v", command, args)
	stdcall(command, args...)
}

func pageIndex(name string) (content []byte) {
	statikFS, err := fs.New()
	if err != nil {
		log.Fatal(err)
	}
	r, err := statikFS.Open(name)
	if err != nil {
		return
	}
	defer r.Close()
	content, err = ioutil.ReadAll(r)
	if err != nil {
		log.Println("statik file err:", err)
		return
	}
	return content
}

func ws(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()

	connID := c.Request.RemoteAddr + c.Request.RequestURI
	wsconns[connID] = ws
	defer func() { delete(wsconns, connID) }()

	log.Println("receive websocket conn:", connID)
	for {
		mtype, message, err := ws.ReadMessage()
		if err != nil {
			log.Println(err)
			break
		}
		log.Printf("ws msg: %s", message)
		var info []byte
		if string(message) == "hi" {
			info = []byte("ok<br>")
		}
		if err := ws.WriteMessage(mtype, info); err != nil {
			log.Println(err)
			break
		}
	}
}

func stdcall(command string, args ...string) {
	cmd := exec.Command(command, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	scanner1 := bufio.NewScanner(stdout)
	scanner2 := bufio.NewScanner(stderr)
	cmd.Start()
	scanner1.Split(bufio.ScanLines)
	scanner2.Split(bufio.ScanLines)
	for scanner1.Scan() {
		fmt.Println(">", scanner1.Text())
		websocketWrite(append([]byte(" > "), scanner1.Bytes()...))
	}

	for scanner2.Scan() {
		fmt.Println(scanner2.Text())
		websocketWrite(append([]byte("x> "), scanner2.Bytes()...))
	}
	cmd.Wait()
}

func websocketWrite(msg []byte) {
	for key, conn := range wsconns {
		mtype := websocket.TextMessage
		if err := conn.WriteMessage(mtype, msg); err != nil {
			log.Printf("%s: %v", key, err)
		}
		if err := conn.WriteMessage(mtype, []byte("<br>")); err != nil {
			log.Printf("%s: %v", key, err)
		}
	}
}
