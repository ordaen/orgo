package sessions

import (
	"errors"
	"time"
	"uuid"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/repo"
	"github.com/ordaen/orgo/types"
)

const (
	// renewWindow is the time before the expiration when a used session is renewed
	renewWindow = 20 * time.Minute
	// renewHours is the hours a renewed session is valid from its renewal
	renewHours = 2
)

var Sessions = repo.RegisterCached(&sessionsStore{})

type sessionsStore struct {
	repo.Cached[*Session]
}

// Find returns the valid session of the ID and the type, or nil
func (c *sessionsStore) Find(id, kind string) *Session {
	return c.findValid(func(s *Session) bool {
		return s.ID == model.UUID(id) && s.Type == kind
	})
}

// FindByToken returns the valid session of the token and the type, or nil
func (c *sessionsStore) FindByToken(token, kind string) *Session {
	return c.findValid(func(s *Session) bool {
		return s.Token == token && s.Type == kind
	})
}

// FindBySSO returns the valid session of the SSO token, or nil
func (c *sessionsStore) FindBySSO(token string) *Session {
	if token == "" {
		return nil
	}
	return c.findValid(func(s *Session) bool {
		return s.SSO == token
	})
}

// FindByUser returns a valid session of the user, or nil
func (c *sessionsStore) FindByUser(user *types.User) *Session {
	if user == nil {
		return nil
	}
	return c.findValid(func(s *Session) bool {
		return s.ofUser(user)
	})
}

// DeleteByUser deletes the sessions of the user
func (c *sessionsStore) DeleteByUser(user *types.User) error {
	if user == nil {
		return nil
	}
	return c.deleteMatching(func(s *Session) bool {
		return s.ofUser(user)
	})
}

// DeleteExpired deletes the expired sessions
func (c *sessionsStore) DeleteExpired() error {
	now := time.Now()
	return c.deleteMatching(func(s *Session) bool {
		return s.expiredAt(now)
	})
}

// findValid returns the session matching match after CheckSession, or nil
func (c *sessionsStore) findValid(match func(*Session) bool) *Session {
	rec, err := c.FindCached(match)
	if err != nil {
		return nil
	}
	return rec.CheckSession()
}

func (c *sessionsStore) deleteMatching(match func(*Session) bool) error {
	var errs []error
	for _, s := range c.FindManyCached(match) {
		errs = append(errs, c.Delete(s))
	}
	return errors.Join(errs...)
}

// CreateSession creates session for user, valid for hours
func (c *sessionsStore) CreateSession(user *types.User, kind string, hours uint) (*Session, error) {
	return c.create(user, kind, "", time.Now().Add(time.Duration(hours)*time.Hour))
}

// CreateSessionSSO creates login session for user, authenticated with the SSO token, valid until expires
func (c *sessionsStore) CreateSessionSSO(user *types.User, token string, expires *time.Time) (*Session, error) {
	if token == "" {
		return nil, errors.New("sso token can't be blank")
	}
	if expires == nil {
		return nil, errors.New("sso session expiration can't be blank")
	}
	return c.create(user, "login", token, *expires)
}

func (c *sessionsStore) create(user *types.User, kind, sso string, expires time.Time) (*Session, error) {
	sess := &Session{
		ID:      model.UUID(uuid.NewV4().String()),
		Token:   uuid.NewV4().String(),
		Type:    kind,
		SSO:     sso,
		Expires: expires,
	}
	sess.SetUserFields(user)
	return c.Create(sess)
}

// Session base model
type Session struct {
	model.Base[model.UUID]
	Token   string
	Type    string
	SSO     string
	Super   bool
	Expires time.Time

	types.UserFields
}

func (m *Session) TableName() string {
	return "sessions"
}

// Delete deletes the session
func (m *Session) Delete() error {
	return Sessions.Delete(m)
}

// WithSSO returns true if the session is authenticated with SSO
func (m *Session) WithSSO() bool {
	return m.SSO != ""
}

// Expired returns true if expired
func (m *Session) Expired() bool {
	return m.expiredAt(time.Now())
}

func (m *Session) expiredAt(now time.Time) bool {
	return now.After(m.Expires)
}

func (m *Session) ofUser(user *types.User) bool {
	return m.UserID == user.ID && m.UserType == user.Type && m.UserIP == user.IP
}

// CheckSession returns the session, or nil after deleting it when it is expired.
// A session expiring within 20 minutes is renewed for 2 hours.
func (m *Session) CheckSession() *Session {
	if m.Expired() {
		m.Delete()
		return nil
	}
	if time.Until(m.Expires) <= renewWindow {
		m.Expand(renewHours)
	}
	return m
}

// Expand sets the session expiration to hours from now
func (m *Session) Expand(hours int) error {
	m.Expires = time.Now().Add(time.Duration(hours) * time.Hour)
	_, err := Sessions.Update(m, "expires")
	return err
}
