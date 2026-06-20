package db

import (
	"database/sql"
	"time"
)

type TrainingRecord struct {
	ID              int64  `json:"id"`
	Tool            string `json:"tool"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         *int64 `json:"ended_at,omitempty"`
	DurationSeconds int    `json:"duration_seconds"`
	Note            string `json:"note"`
}

type TrainingStats struct {
	Dates []TrainingDate `json:"dates"`
}

type TrainingDate struct {
	Date     string            `json:"date"`
	Total    int               `json:"total_seconds"`
	Sessions []TrainingSession `json:"sessions"`
}

type TrainingSession struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Duration int    `json:"duration_seconds"`
}

func (s *SQLiteStore) StartTraining(tool string) (int64, error) {
	now := time.Now().Unix()
	result, err := s.db.Exec(`INSERT INTO training_records (tool, started_at, duration_seconds) VALUES (?, ?, 0)`,
		tool, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *SQLiteStore) StopTraining(id int64) error {
	now := time.Now().Unix()
	var started int64
	err := s.db.QueryRow(`SELECT started_at FROM training_records WHERE id = ?`, id).Scan(&started)
	if err != nil {
		return err
	}
	dur := int(now - started)
	_, err = s.db.Exec(`UPDATE training_records SET ended_at=?, duration_seconds=? WHERE id=?`,
		now, dur, id)
	return err
}

func (s *SQLiteStore) GetTrainingStats(tool string, days int) (*TrainingStats, error) {
	since := time.Now().AddDate(0, 0, -days+1).Unix()

	rows, err := s.db.Query(`SELECT id, started_at, ended_at, duration_seconds FROM training_records WHERE tool=? AND started_at >= ? ORDER BY started_at`,
		tool, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []struct {
		id    int64
		start int64
		end   sql.NullInt64
		dur   int
	}
	for rows.Next() {
		var r struct {
			id    int64
			start int64
			end   sql.NullInt64
			dur   int
		}
		if err := rows.Scan(&r.id, &r.start, &r.end, &r.dur); err != nil {
			continue
		}
		records = append(records, r)
	}

	dateMap := make(map[string]*TrainingDate)
	for i := 0; i < days; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		dateMap[day] = &TrainingDate{Date: day}
	}

	for _, r := range records {
		startT := time.Unix(r.start, 0)
		day := startT.Format("2006-01-02")
		td, ok := dateMap[day]
		if !ok {
			continue
		}
		td.Total += r.dur
		endStr := ""
		if r.end.Valid {
			endT := time.Unix(r.end.Int64, 0)
			endStr = endT.Format("15:04")
		}
		td.Sessions = append(td.Sessions, TrainingSession{
			Start:    startT.Format("15:04"),
			End:      endStr,
			Duration: r.dur,
		})
	}

	result := &TrainingStats{}
	for i := days - 1; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		td, ok := dateMap[day]
		if !ok {
			result.Dates = append(result.Dates, TrainingDate{Date: day})
		} else {
			result.Dates = append(result.Dates, *td)
		}
	}
	return result, nil
}

func (s *SQLiteStore) GetActiveTraining(tool string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM training_records WHERE tool=? AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, tool).Scan(&id)
	if err != nil {
		return 0, nil
	}
	return id, nil
}
