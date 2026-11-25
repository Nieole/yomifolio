// Package delegated owns independent MCP credentials and current delegated authority.
// Protocol adapters consume these use cases without obtaining database access.
package delegated

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"yomifolio/internal/bookcontent"
)

var (
	ErrDenied  = errors.New("delegated access denied")
	ErrInvalid = errors.New("invalid credential restrictions")
)

const (
	Read          = "read"
	MetadataWrite = "metadata_write"
	ScanControl   = "scan_control"
	InternalLLM   = "internal_llm"
	Cleanup       = "cleanup"
)

// Subject is durable attribution, not a cached authorization decision.
// Every operation/checkpoint must call Authorize again with its actual library and capability.
type Subject struct {
	UserID       int64  `json:"user_id"`
	CredentialID string `json:"credential_id"`
}
type Restrictions struct {
	UserID       int64    `json:"user_id"`
	Name         string   `json:"name"`
	LibraryIDs   []int64  `json:"library_ids"`
	Capabilities []string `json:"capabilities"`
}
type Credential struct {
	ID string `json:"id"`
	Restrictions
	Revoked bool `json:"revoked"`
}
type IssuedCredential struct {
	Credential
	Token string `json:"token"`
}

// Series deliberately excludes host paths, reading state and arbitrary file access.
type Series struct {
	ID            int64  `json:"id"`
	LibraryID     int64  `json:"library_id"`
	Name          string `json:"name"`
	Title         string `json:"title"`
	DirectoryName string `json:"directory_name"`
}

type Authority struct {
	Credential         Credential
	Admin              bool
	MustChangePassword bool
}
type Write interface {
	Admin(context.Context, int64) error
	UserExists(context.Context, int64) error
	LibraryExists(context.Context, int64) error
	Insert(context.Context, Credential, string) error
	List(context.Context) ([]Credential, error)
	Get(context.Context, string) (Credential, error)
	Update(context.Context, Credential) error
}
type Store interface {
	Write(context.Context, func(Write) error) error
	Authenticate(context.Context, string) (Subject, error)
	Authority(context.Context, Subject) (Authority, error)
	Series(context.Context, int64) (Series, error)
}
type Service struct {
	store    Store
	content  *bookcontent.Service
	access   bookcontent.LibraryAccess
	runs     *RunAccess
	matching *MatchingAccess
	models   *ModelBudgetAccess
}

func New(store Store) *Service { return &Service{store: store} }
func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func validate(in Restrictions) error {
	if in.UserID <= 0 || strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || len(in.LibraryIDs) > 256 || len(in.Capabilities) > 5 {
		return ErrInvalid
	}
	for _, id := range in.LibraryIDs {
		if id <= 0 {
			return ErrInvalid
		}
	}
	for _, capability := range in.Capabilities {
		if !slices.Contains([]string{Read, MetadataWrite, ScanControl, InternalLLM, Cleanup}, capability) {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) Issue(ctx context.Context, actor int64, in Restrictions) (IssuedCredential, error) {
	if err := validate(in); err != nil {
		return IssuedCredential{}, err
	}
	token, err := randomToken()
	if err != nil {
		return IssuedCredential{}, err
	}
	id, err := randomToken()
	if err != nil {
		return IssuedCredential{}, err
	}
	in.LibraryIDs = slices.Clone(in.LibraryIDs)
	if in.LibraryIDs == nil {
		in.LibraryIDs = []int64{}
	}
	in.Capabilities = slices.Clone(in.Capabilities)
	if in.Capabilities == nil {
		in.Capabilities = []string{}
	}
	out := IssuedCredential{Credential: Credential{ID: id, Restrictions: in}, Token: "ymcp_" + token}
	err = s.store.Write(ctx, func(w Write) error {
		if err := w.Admin(ctx, actor); err != nil {
			return err
		}
		if err := w.UserExists(ctx, in.UserID); err != nil {
			return err
		}
		for _, id := range in.LibraryIDs {
			if err := w.LibraryExists(ctx, id); err != nil {
				return err
			}
		}
		return w.Insert(ctx, out.Credential, digest(out.Token))
	})
	if err != nil {
		return IssuedCredential{}, err
	}
	return out, nil
}
func (s *Service) List(ctx context.Context, actor int64) ([]Credential, error) {
	var out []Credential
	err := s.store.Write(ctx, func(w Write) error {
		if err := w.Admin(ctx, actor); err != nil {
			return err
		}
		var err error
		out, err = w.List(ctx)
		return err
	})
	return out, err
}
func (s *Service) Narrow(ctx context.Context, actor int64, id string, in Restrictions) (Credential, error) {
	if err := validate(in); err != nil {
		return Credential{}, err
	}
	var out Credential
	err := s.store.Write(ctx, func(w Write) error {
		if err := w.Admin(ctx, actor); err != nil {
			return err
		}
		old, err := w.Get(ctx, id)
		if err != nil {
			return ErrDenied
		}
		if old.Revoked || old.UserID != in.UserID || old.Name != in.Name {
			return ErrInvalid
		}
		for _, v := range in.LibraryIDs {
			if !slices.Contains(old.LibraryIDs, v) {
				return ErrInvalid
			}
		}
		for _, v := range in.Capabilities {
			if !slices.Contains(old.Capabilities, v) {
				return ErrInvalid
			}
		}
		old.LibraryIDs = in.LibraryIDs
		old.Capabilities = in.Capabilities
		if old.LibraryIDs == nil {
			old.LibraryIDs = []int64{}
		}
		if old.Capabilities == nil {
			old.Capabilities = []string{}
		}
		out = old
		return w.Update(ctx, out)
	})
	return out, err
}
func (s *Service) Revoke(ctx context.Context, actor int64, id string) error {
	return s.store.Write(ctx, func(w Write) error {
		if err := w.Admin(ctx, actor); err != nil {
			return err
		}
		old, err := w.Get(ctx, id)
		if err != nil {
			return ErrDenied
		}
		old.Revoked = true
		return w.Update(ctx, old)
	})
}
func (s *Service) Authenticate(ctx context.Context, token string) (Subject, error) {
	if !strings.HasPrefix(token, "ymcp_") || len(token) != 69 {
		return Subject{}, ErrDenied
	}
	return s.store.Authenticate(ctx, digest(token))
}
func (s *Service) Authorize(ctx context.Context, subject Subject, library int64, capability string) error {
	authority, err := s.store.Authority(ctx, subject)
	if err != nil {
		return ErrDenied
	}
	return authorizeAuthority(authority, library, capability)
}

// GrantedCapabilities reports the flags the credential currently carries, for deciding which
// tools a listing may show. Visibility follows the granted flags, not the account's own
// permissions: a regular account still lists the scan tools its credential holds and every call
// stays denied by Authorize. Each request re-reads them, so narrowing and revocation apply to the
// next listing.
func (s *Service) GrantedCapabilities(ctx context.Context, subject Subject) ([]string, error) {
	authority, err := s.store.Authority(ctx, subject)
	if err != nil || authority.Credential.Revoked || authority.MustChangePassword {
		return nil, ErrDenied
	}
	return authority.Credential.Capabilities, nil
}

func authorizeAuthority(authority Authority, library int64, capability string) error {
	if authority.Credential.Revoked || authority.MustChangePassword {
		return ErrDenied
	}
	if !slices.Contains(authority.Credential.LibraryIDs, library) || !slices.Contains(authority.Credential.Capabilities, capability) {
		return ErrDenied
	}
	// Regular accounts have no delegated management or paid-model permission.
	if capability != Read && !authority.Admin {
		return ErrDenied
	}
	return nil
}
func (s *Service) GetSeries(ctx context.Context, subject Subject, id int64) (Series, error) {
	series, err := s.store.Series(ctx, id)
	if err != nil {
		return Series{}, ErrDenied
	}
	if err := s.Authorize(ctx, subject, series.LibraryID, Read); err != nil {
		return Series{}, err
	}
	return series, bounded(series)
}
