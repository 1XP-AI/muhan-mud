package game

type CreationStage uint8

const (
	CreationGender CreationStage = iota
	CreationClass
	CreationStats
	CreationWeapon
	CreationAlignment
	CreationRace
	CreationReady
	CreationCanceled
)

// CreationWizard handles only the original six character choices, after name
// confirmation. Ready means ready for credential/persistence processing, NOT
// authenticated or admitted to the game. No password is retained here.
type CreationWizard struct {
	stage   CreationStage
	choices CreationChoices
	draft   Creation
}

func NewCreationWizard() *CreationWizard       { return &CreationWizard{} }
func (w *CreationWizard) Stage() CreationStage { return w.stage }
func (w *CreationWizard) Draft() (Creation, bool) {
	if w.stage != CreationReady {
		return Creation{}, false
	}
	return w.draft, true
}
func (w *CreationWizard) Cancel() {
	w.choices = CreationChoices{}
	w.draft = Creation{}
	w.stage = CreationCanceled
}

func (w *CreationWizard) Prompt() string {
	switch w.stage {
	case CreationGender:
		return "당신은 남자입니까, 여자입니까(남자/여자)? "
	case CreationClass:
		return "1.자객  2.권법가  3.불제자  4.검사\r\n5.도술사  6.무사  7.포졸  8.도둑\r\n직업을 고르세요: "
	case CreationStats:
		return "54점으로 능력치를 구성하십시오(각 3~18).\r\n힘 민첩 맷집 지식 신앙심\r\n예: 12 10 12 10 10\r\n: "
	case CreationWeapon:
		return "익숙한 무기를 고르십시오.\r\n1.도  2.검  3.봉  4.창  5.궁\r\n: "
	case CreationAlignment:
		return "선한 구성원은 다른 사람과 공격/도둑질을 할 수 없습니다.\r\n악한 구성원은 다른 악한 구성원과 공격/도둑질을 할 수 있습니다.\r\n성향을 고르십시오(선함/악함): "
	case CreationRace:
		return "1.난장이족  2.용신족  3.땅귀신족  4.요괴족\r\n5.거인족  6.토신족  7.인간족  8.도깨비족\r\n종족을 고르십시오: "
	default:
		return ""
	}
}

func menuChoice(input string, max int) (int, bool) {
	if len(input) != 1 || input[0] < '1' || int(input[0]-'0') > max {
		return 0, false
	}
	return int(input[0] - '0'), true
}

func (w *CreationWizard) Submit(input string) error {
	next := w.choices
	switch w.stage {
	case CreationGender:
		switch input {
		case "남", "남자":
			next.Male = true
		case "여", "여자":
			next.Male = false
		default:
			return ErrCreation
		}
	case CreationClass:
		value, ok := menuChoice(input, 8)
		if !ok {
			return ErrCreation
		}
		next.Class = value
	case CreationStats:
		value, err := ParseCreationStats(input)
		if err != nil {
			return err
		}
		next.Stats = value
	case CreationWeapon:
		value, ok := menuChoice(input, 5)
		if !ok {
			return ErrCreation
		}
		next.Weapon = value
	case CreationAlignment:
		switch input {
		case "선", "선함":
			next.Chaotic = false
		case "악", "악함":
			next.Chaotic = true
		default:
			return ErrCreation
		}
	case CreationRace:
		value, ok := menuChoice(input, 8)
		if !ok {
			return ErrCreation
		}
		next.RaceChoice = value
		draft, err := BuildCreation(next)
		if err != nil {
			return err
		}
		w.draft = draft
	default:
		return ErrCreation
	}
	w.choices = next
	w.stage++
	return nil
}
