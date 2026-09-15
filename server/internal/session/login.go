// Package session owns serial, connection-scoped terminal conversations.
package session

import (
	"context"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

type Accounts interface {
	Exists(context.Context, string) (bool, error)
	Register(context.Context, string, []byte, game.Creation) (string, error)
	Authenticate(context.Context, string, []byte) (storage.Character, error)
}

// Verified is a credential-verified draft, not authorization to enter a world.
// Secret controls input echo for the NEXT line; never write that line to output.
type View struct {
	Text           string
	Secret, Closed bool
	Verified       *storage.Character
}
type loginStage uint8

const (
	nameStage loginStage = iota
	confirmStage
	enterStage
	wizardStage
	newPasswordStage
	passwordStage
	verifiedStage
	closedStage
)

type Login struct {
	db       Accounts
	stage    loginStage
	name     string
	wizard   *game.CreationWizard
	attempts int
	view     View
}

func NewLogin(db Accounts) *Login {
	return &Login{db: db, view: View{Text: "당신의 이름은 무엇입니까? "}}
}
func (s *Login) View() View {
	result := s.view
	if result.Verified != nil {
		value := *result.Verified
		result.Verified = &value
	}
	return result
}
func (s *Login) Close() {
	if s.wizard != nil {
		s.wizard.Cancel()
		s.wizard = nil
	}
	s.name = ""
	s.stage = closedStage
	s.view = View{Text: "접속을 끊습니다.\r\n", Closed: true}
}
func (s *Login) failed() View {
	s.Close()
	s.view.Text = "처리하지 못했습니다. 잠시 후 다시 접속하십시오.\r\n"
	return s.View()
}

func (s *Login) Submit(ctx context.Context, line string) View {
	if s.stage == closedStage || s.stage == verifiedStage {
		return s.View()
	}
	if ctx.Err() != nil {
		return s.failed()
	}
	switch s.stage {
	case nameStage:
		name, err := identity.CanonicalName(line)
		if err != nil {
			s.view = View{Text: "이름을 다시 입력하십시오.\r\n당신의 이름은 무엇입니까? "}
			break
		}
		exists, err := s.db.Exists(ctx, name)
		if err != nil {
			return s.failed()
		}
		s.name = name
		if exists {
			s.stage = passwordStage
			s.view = View{Text: "암호를 넣어 주십시오: ", Secret: true}
		} else {
			s.stage = confirmStage
			s.view = View{Text: "이 이름으로 새 캐릭터를 만드시겠습니까(예/아니오)? "}
		}
	case confirmStage:
		if line != "예" && !strings.EqualFold(line, "y") {
			s.name = ""
			s.stage = nameStage
			s.view = View{Text: "당신의 이름은 무엇입니까? "}
			break
		}
		s.stage = enterStage
		s.view = View{Text: "[엔터]를 누르십시오."}
	case enterStage:
		if line != "" {
			break
		}
		s.wizard = game.NewCreationWizard()
		s.stage = wizardStage
		s.view = View{Text: s.wizard.Prompt()}
	case wizardStage:
		if err := s.wizard.Submit(line); err != nil {
			s.view = View{Text: "입력이 잘못되었습니다.\r\n" + s.wizard.Prompt()}
			break
		}
		if s.wizard.Stage() == game.CreationReady {
			s.stage = newPasswordStage
			s.view = View{Text: "새 암호를 넣으십시오(3~14바이트): ", Secret: true}
		} else {
			s.view = View{Text: s.wizard.Prompt()}
		}
	case newPasswordStage:
		draft, ok := s.wizard.Draft()
		if !ok {
			return s.failed()
		}
		password := []byte(line)
		id, err := s.db.Register(ctx, s.name, password, draft)
		clear(password)
		if errors.Is(err, identity.ErrPassword) {
			s.view = View{Text: "암호를 다시 넣으십시오(3~14바이트): ", Secret: true}
			break
		}
		if err != nil || id == "" {
			return s.failed()
		}
		s.wizard.Cancel()
		s.wizard = nil
		s.stage = verifiedStage
		s.view = View{Text: "캐릭터가 저장되었습니다.\r\n", Verified: &storage.Character{ID: id, Draft: draft}}
	case passwordStage:
		password := []byte(line)
		character, err := s.db.Authenticate(ctx, s.name, password)
		clear(password)
		if errors.Is(err, storage.ErrCredentials) {
			s.attempts++
			if line == "" || s.attempts >= 3 {
				s.Close()
			} else {
				s.view = View{Text: "암호가 틀립니다. 다시 입력하십시오: ", Secret: true}
			}
			break
		}
		if err != nil || character.ID == "" {
			return s.failed()
		}
		s.stage = verifiedStage
		s.view = View{Text: "암호를 확인했습니다.\r\n", Verified: &character}
	}
	return s.View()
}
