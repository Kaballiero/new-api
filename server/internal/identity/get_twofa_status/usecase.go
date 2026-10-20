package get_twofa_status

type Reader interface {
	Factor(userID int) (FactorState, error)
	BackupCount(userID int) (int, error)
	LogCountFailure(error)
}

func (usecase Usecase) Execute(userID int) (Output, error) {
	factor, err := usecase.reader.Factor(userID)
	if err != nil {
		return nil, err
	}
	status := Output{"enabled": false, "locked": false}
	if factor.Present {
		status["enabled"] = factor.Enabled
		status["locked"] = factor.Locked
		if factor.Enabled {
			count, err := usecase.reader.BackupCount(userID)
			if err != nil {
				usecase.reader.LogCountFailure(err)
			} else {
				status["backup_codes_remaining"] = count
			}
		}
	}
	return status, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}
