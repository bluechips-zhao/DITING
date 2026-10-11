package db

import "encoding/json"

func (s *ExplorationStore) ReportContext() (goals []*Node, states map[string]int, err error) {
	goals = []*Node{}
	states = map[string]int{}
	rows, err := s.db.Query(`SELECT id,payload,state FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal' ORDER BY id`, s.expID)
	if err != nil {
		return
	}
	for rows.Next() {
		n := &Node{Kind: KindGoal}
		var raw []byte
		if err = rows.Scan(&n.ID, &raw, &n.State); err != nil {
			rows.Close()
			return
		}
		n.Payload = json.RawMessage(raw)
		goals = append(goals, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	rows, err = s.db.Query(`SELECT state,COUNT(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind='intent' GROUP BY state`, s.expID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			return
		}
		states[state] = count
	}
	err = rows.Err()
	return
}
