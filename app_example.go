package main

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sedmess/go-ctx-base/actuator"
	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx-base/logconfig"
	_ "github.com/sedmess/go-ctx-base/logconfig"
	"github.com/sedmess/go-ctx-base/scheduler"
	"github.com/sedmess/go-ctx-base/utils/channels"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
	"github.com/sedmess/go-ctx/u"
	"gorm.io/gorm"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type controllerSecurity struct {
	l      logger.Logger         `ctx:""`
	server httpserver.RestServer `ctx:""`
	tokens map[string]bool       `env:"HTTP_AUTH_TOKENS"`
}

func (s *controllerSecurity) Init() {
	s.server.AddMiddleware(httpserver.BearerTokenAuthenticator(func(path string, token string) httpserver.AuthenticationResultCode {
		if strings.HasPrefix(path, "/actuator") {
			return httpserver.Authorized
		}
		if token == "" {
			return httpserver.AuthenticationRequired
		}
		if s.tokens[token] {
			return httpserver.Authorized
		} else {
			return httpserver.Forbidden
		}
	}))
}

type Message struct {
	Id         int64     `gorm:"primaryKey,autoIncrement"`
	RecCreated time.Time `gorm:"autoCreateTime"`
	Receiver   string    `gorm:"index"`
	Sender     string
	Text       string
}

type messageController struct {
	l      logger.Logger         `ctx:""`
	server httpserver.RestServer `ctx:""`

	messageService *messageService `ctx:""`

	newMessagesCounter prometheus.Counter
}

func (c *messageController) Init() {
	httpserver.BuildTypedRoute[string](c.server).Method(http.MethodPost).Path("/messages").Handler(c.newMessage)
	httpserver.BuildRoute(c.server).Method(http.MethodGet).Path("/messages").Handler(c.getMessages)

	c.newMessagesCounter = promauto.NewCounter(prometheus.CounterOpts{
		Name: "new_messages_total",
	})
}

func (c *messageController) newMessage(request *httpserver.RequestData, body string) (rs httpserver.Response) {
	from := request.Query().Get("from")
	to := request.Query().Get("to")

	if !rs.VerifyNotEmpty(from, to) {
		return
	}

	if err := c.messageService.SaveMessage(from, to, body); err != nil {
		rs.Error(err)
		return
	} else {
		c.newMessagesCounter.Inc()
		rs.Status(http.StatusCreated)
		return
	}
}

func (c *messageController) getMessages(request *httpserver.RequestData) (rs httpserver.Response) {
	to := request.Query().Get("to")
	sinceStr := request.Query().Get("since")

	if !rs.VerifyNotEmpty(to, sinceStr) {
		return
	}

	since, err := strconv.ParseInt(sinceStr, 10, 64)
	if err != nil {
		rs.BadRequest()
		return
	}

	if messages, err := c.messageService.GetMessages(to, since).CollectToSlice(); err != nil {
		rs.Error(err)
		return
	} else {
		rs.Content(messages)
		return
	}
}

type messageService struct {
	l  logger.Logger `ctx:""`
	db db.Connection `ctx:""`

	messageTTL         time.Duration        `env:"MESSAGE_TTL=24h"`
	messageCleanupCron string               `env:"MESSAGE_CLEANUP_CRON=0 0 * * * *"`
	scheduler          *scheduler.Scheduler `ctx:""`
}

func (s *messageService) Init() {
	s.db.AutoMigrate(&Message{})

	u.Must2(s.scheduler.ScheduleTaskCron(s.messageCleanupCron, "messages-cleanup", func() {
		if err := s.removeMessagesBefore(time.Now().Add(-s.messageTTL)); err != nil {
			s.l.Error("cleanup task failed:", err)
		}
	}))
}

func (s *messageService) SaveMessage(from string, to string, text string) error {
	return s.db.Session(func(session *db.Session) error {
		return session.Tx(func(session *db.Session) error {
			message := Message{
				RecCreated: time.Now(),
				Sender:     from,
				Receiver:   to,
				Text:       text,
			}
			s.l.Debug("storing message: from =", from, ", to =", to)
			return session.Save(&message).Error
		})
	})
}

func (s *messageService) GetMessages(to string, since int64) channels.StreamingChan[Message] {
	return db.SessionStream[Message](s.db, 2, func(session *gorm.DB) *gorm.DB {
		return session.Where("receiver = ?", to).Where("id > ?", since).Order("id asc")
	})
}

func (s *messageService) removeMessagesBefore(time time.Time) error {
	return s.db.Session(func(session *db.Session) error {
		return session.Tx(func(session *db.Session) error {
			result := session.Where("rec_created < ?", time).Delete(&Message{})
			if result.Error != nil {
				s.l.Error("on deleting messages before", time, ":", result.Error)
				return result.Error
			} else {
				s.l.Info("deleted", result.RowsAffected, "rows before", time)
				return nil
			}
		})
	})
}

type fsController struct {
	server httpserver.RestServer `ctx:""`
}

func (c *fsController) Init() {
	fileServerHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("./")))
	httpserver.BuildRoute(c.server).Method("GET").Path("/static/*").HandlerRaw(func(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
		fileServerHandler.ServeHTTP(responseWriter.(http.ResponseWriter), request.Request)
		return nil
	})
}

var Packages = []ctx.ServicePackage{
	httpserver.Default(),
	db.Default(),
	scheduler.Default(),
	ctx.PackageOf(
		&controllerSecurity{},
		actuator.AddToDefaultHttpServer(),
		&messageController{},
		&messageService{},
		&fsController{},
	),
}

func main() {
	logconfig.InitWithExtraHandlers(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	ctx.CreateContextualizedApplication(Packages...).Join()
}
