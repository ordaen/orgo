package types

import "github.com/ordaen/orgo/model"

// User is a user acting on records, with the IP address of the request. RoleID, Email and Avatar are not stored
// by UserFields.
type User struct {
	ID     model.ID `json:"id,omitempty"`
	RoleID model.ID `json:"role_id,omitempty"`
	Email  string   `json:"email,omitempty"`
	Name   string   `json:"name,omitempty"`
	IP     string   `json:"ip,omitempty"`
	Type   string   `json:"type,omitempty"`
	Avatar string   `json:"avatar,omitempty"`
}

// UserFields is embedded in the records storing the user that created them, see SetUserFields.
type UserFields struct {
	UserID   model.ID `json:"user_id,omitempty,readonly"`
	UserName string   `json:"user_name,omitempty,readonly"`
	UserIP   string   `json:"user_ip,omitempty,readonly"`
	UserType string   `json:"user_type,omitempty,readonly"`
}

// User returns the stored user.
func (m UserFields) User() *User {
	return &User{ID: m.UserID, Name: m.UserName, IP: m.UserIP, Type: m.UserType}
}

// SetUserFields sets the fields to the user, a nil user leaves them unchanged.
func (m *UserFields) SetUserFields(u *User) {
	if u != nil {
		m.UserID = u.ID
		m.UserName = u.Name
		m.UserType = u.Type
		m.UserIP = u.IP
	}
}
