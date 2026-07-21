package main

import (
	"context"
	"errors"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sedmess/go-ctx-base/actuator"
	"github.com/sedmess/go-ctx-base/db"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx-base/logconfig"
	_ "github.com/sedmess/go-ctx-base/logconfig"
	"github.com/sedmess/go-ctx-base/profiler"
	"github.com/sedmess/go-ctx-base/scheduler"
	"github.com/sedmess/go-ctx-base/utils/channels"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/appinfo"
	"github.com/sedmess/go-ctx/ctx/logger"
	"gorm.io/gorm"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// controllerSecurity handles authentication middleware configuration for application routes.
// Actuator and profiler enforce their own component-scoped control-plane policies.
type controllerSecurity struct {
	l      logger.Logger         `ctx:""`
	server httpserver.RestServer `ctx:""`
	tokens map[string]bool       `env:"HTTP_AUTH_TOKENS"`
}

// Init registers bearer authentication for every route on the application HTTP server.
func (s *controllerSecurity) Init() {
	s.server.AddMiddleware(httpserver.BearerTokenAuthenticator(func(_ string, token string) httpserver.AuthenticationResultCode {
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

// Message represents a communication entity stored in the database.
// Contains sender/receiver information and message content with timestamps.
type Message struct {
	Id         int64     `gorm:"primaryKey,autoIncrement"`
	RecCreated time.Time `gorm:"autoCreateTime"`
	Receiver   string    `gorm:"index"`
	Sender     string
	Text       string
}

// messageController handles HTTP endpoints for message management.
// Exposes REST API endpoints for creating and retrieving messages.
type messageController struct {
	l      logger.Logger         `ctx:""`
	server httpserver.RestServer `ctx:""`

	messageService MessageService `ctx:""`

	newMessagesCounter prometheus.Counter
}

var sharedNewMessagesCounter = sync.OnceValue(func() prometheus.Counter {
	return promauto.NewCounter(prometheus.CounterOpts{Name: "new_messages_total"})
})

// Init registers the message controller's HTTP routes and initializes metrics collection.
// Sets up POST /messages and GET /messages endpoints.
func (c *messageController) Init() {
	httpserver.BuildTypedRoute[string](c.server).Method(http.MethodPost).Path("/messages").Handler(c.newMessage)
	httpserver.BuildRoute(c.server).Method(http.MethodGet).Path("/messages").Handler(c.getMessages)

	c.newMessagesCounter = sharedNewMessagesCounter()
}

// newMessage handles message creation requests. Validates required from/to parameters,
// stores messages via service layer, and tracks metrics for new messages.
// Returns 400 for invalid requests, 500 on storage errors, 201 on success.
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

// getMessages retrieves messages for a specific receiver. Requires 'to' and 'since' parameters.
// Returns messages as a JSON array or appropriate error status codes.
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

type MessageService interface {
	SaveMessage(from string, to string, text string) error
	GetMessages(to string, since int64) channels.StreamingChan[Message]
}

// messageService provides business logic for message storage and retrieval.
// Handles database operations and scheduled cleanup of old messages.
type messageService struct {
	MessageService `ctx:"impl"`

	l  logger.Logger `ctx:""`
	db db.Connection `ctx:""`

	messageTTL         time.Duration        `env:"MESSAGE_TTL=24h"`
	messageCleanupCron string               `env:"MESSAGE_CLEANUP_CRON=0 0 * * * *"`
	scheduler          *scheduler.Scheduler `ctx:""`
}

// Init initializes the message service by creating database tables,
// and scheduling periodic message cleanup tasks.
func (s *messageService) Init() error {
	s.db.AutoMigrate(&Message{})

	if _, err := s.scheduler.ScheduleTaskCronContext(s.messageCleanupCron, "messages-cleanup", func(taskContext context.Context) {
		if err := s.removeMessagesBefore(taskContext, time.Now().Add(-s.messageTTL)); err != nil &&
			!errors.Is(err, context.Canceled) {
			s.l.Error("cleanup task failed:", err)
		}
	}); err != nil {
		return err
	}

	return nil
}

// SaveMessage persists a new message to the database within a transaction.
// Validates input parameters before storage.
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

// GetMessages retrieves messages for a recipient using a streaming channel.
// Uses paginated database access with a fetch size of 2 for efficient memory usage.
func (s *messageService) GetMessages(to string, since int64) channels.StreamingChan[Message] {
	return db.SessionStream[Message](s.db, 2, func(session *gorm.DB) *gorm.DB {
		return session.Where("receiver = ?", to).Where("id > ?", since).Order("id asc")
	})
}

// removeMessagesBefore deletes messages older than specified time.
// Used by the scheduled cleanup task to maintain database size.
func (s *messageService) removeMessagesBefore(taskContext context.Context, time time.Time) error {
	return s.db.SessionContext(taskContext, func(session *db.Session) error {
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

// fsController serves static files from the server's current directory.
// Exposes GET /static/* endpoint for file serving.
type fsController struct {
	server httpserver.RestServer `ctx:""`
}

// Init registers the static file server route with the HTTP server.
// Serves files from the application's working directory under /static/ path.
func (c *fsController) Init() {
	fileServerHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("./")))
	httpserver.BuildRoute(c.server).Path("/static/*").HandlerRaw(func(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
		fileServerHandler.ServeHTTP(responseWriter.(http.ResponseWriter), request.Request)
		return nil
	})
}

// Packages defines the application's service components and dependencies.
// Aggregates HTTP server, database, scheduler, and custom controllers.
var Packages = []ctx.ServicePackage{
	httpserver.Default(),
	db.Default(),
	scheduler.Default(),
	actuator.RunAsIndependentServer(),
	profiler.RunAsIndependentServer(),
	ctx.PackageOf(
		&controllerSecurity{},
		&messageController{},
		&messageService{},
		&fsController{},
	),
}

// main is the application entry point that initializes logging and starts the contextualized application.
// Configures JSON logging with debug level and source location tracking.
func main() {
	appinfo.Name = "app_example"
	logconfig.InitWithExtraHandlers(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}))
	ctx.CreateContextualizedApplication(Packages...).Join()
}
