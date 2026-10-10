package backend

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"linklan/internal/auth"
	"linklan/internal/model"
)

type deviceAccessRequest struct {
	Name, Challenge, Signature, Version string
	PublicKey                           string `json:"public_key"`
}

func (s *Server) deviceAccessChallenge(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		PublicKey string `json:"public_key"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	pub, e := base64.RawStdEncoding.DecodeString(b.PublicKey)
	if e != nil || len(pub) != ed25519.PublicKeySize || base64.RawStdEncoding.EncodeToString(pub) != b.PublicKey {
		return bad("Некорректный ключ устройства")
	}
	challenge := auth.Random(32)
	if e = s.redis.Set(r.Context(), "device-access:"+auth.HashToken(challenge), b.PublicKey, 30*time.Second).Err(); e != nil {
		return e
	}
	output(w, 200, map[string]string{"challenge": challenge})
	return nil
}

func (s *Server) verifyDeviceAccess(ctx context.Context, b deviceAccessRequest) error {
	pub, e := base64.RawStdEncoding.DecodeString(b.PublicKey)
	sig, se := base64.RawStdEncoding.DecodeString(b.Signature)
	if e != nil || se != nil || len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize ||
		b.Name != strings.TrimSpace(b.Name) || len(b.Name) < 1 || len(b.Name) > 64 ||
		len(b.Version) > 32 || len(b.Challenge) != 43 || strings.ContainsAny(b.Name+b.Version, "|\r\n\x00") {
		return bad("Некорректные данные устройства")
	}
	if !ed25519.Verify(pub, []byte(model.DeviceProof(b.Challenge, b.PublicKey, b.Name, b.Version)), sig) {
		return forbidden()
	}
	key, e := s.redis.GetDel(ctx, "device-access:"+auth.HashToken(b.Challenge)).Result()
	if e != nil || key != b.PublicKey {
		return forbidden()
	}
	return nil
}

func (s *Server) deviceAccess(w http.ResponseWriter, r *http.Request) error {
	var b deviceAccessRequest
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if e := s.verifyDeviceAccess(r.Context(), b); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// The same key always returns the same principal, even after a lost response
	// or two first-run requests racing. Existing account-owned devices keep ownership.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, b.PublicKey); e != nil {
		return e
	}
	var u model.User
	var d model.Device
	e = tx.QueryRow(ctx, `SELECT u.id::text,u.email,u.name,u.admin,u.blocked,d.id::text,d.name,d.blocked FROM devices d JOIN users u ON u.id=d.user_id WHERE d.public_key=$1 FOR UPDATE OF d,u`, b.PublicKey).
		Scan(&u.ID, &u.Email, &u.Name, &u.Admin, &u.Blocked, &d.ID, &d.Name, &d.Blocked)
	if errors.Is(e, pgx.ErrNoRows) {
		u = model.User{ID: auth.UUID(), Name: b.Name}
		u.Email = "device-" + u.ID + "@linklan.invalid"
		if _, e = tx.Exec(ctx, `INSERT INTO users(id,email,name,password_hash,device_only) VALUES($1,$2,$3,'!device-key',true)`, u.ID, u.Email, u.Name); e != nil {
			return e
		}
		d.ID = auth.UUID()
		if _, e = tx.Exec(ctx, `INSERT INTO devices(id,user_id,name,public_key,version) VALUES($1,$2,$3,$4,$5)`, d.ID, u.ID, b.Name, b.PublicKey, b.Version); e != nil {
			return e
		}
	} else if e != nil {
		return e
	} else if u.Blocked || d.Blocked {
		return apiError{403, "Устройство заблокировано администратором"}
	} else {
		if _, e = tx.Exec(ctx, `UPDATE devices SET name=$2,version=$3 WHERE id=$1`, d.ID, b.Name, b.Version); e != nil {
			return e
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET name=$2 WHERE id=$1 AND device_only`, u.ID, b.Name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			u.Name = b.Name
		}
	}
	d.Name, d.UserID, d.PublicKey = b.Name, u.ID, b.PublicKey
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	s.audit(ctx, u.ID, "device_access", d.ID)
	return s.issueDevice(w, r, u, &d)
}
