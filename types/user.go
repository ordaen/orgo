package types

import "github.com/ordaen/orgo/model"

// User type
type User struct {
	ID   model.ID `json:"id,omitempty"`
	Name string   `json:"name,omitempty"`
	IP   string   `json:"ip,omitempty"`
	Type string   `json:"type,omitempty"`
}

// UserFields type
type UserFields struct {
	UserID   model.ID `json:"user_id,omitempty,readonly" scope:"admins"`
	UserName string   `json:"user_name,omitempty,readonly"`
	UserIP   string   `json:"user_ip,omitempty,readonly" scope:"admins"`
	UserType string   `json:"user_type,omitempty,readonly"`
}

func (m UserFields) User() *User {
	return &User{ID: m.UserID, Name: m.UserName, IP: m.UserIP, Type: m.UserType}
}

func (m *UserFields) SetUserFields(u *User) {
	if u != nil {
		m.UserID = u.ID
		m.UserName = u.Name
		m.UserType = u.Type
		m.UserIP = u.IP
	}
}
