package db

// RunningAndGoalCounts aggregates the entire local graph without loading nodes.
func (s *ExplorationStore) RunningAndGoalCounts() (running, goals, met int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FILTER(WHERE kind='intent' AND state='running'),
	 COUNT(*) FILTER(WHERE kind='goal'), COUNT(*) FILTER(WHERE kind='goal' AND state='met')
	 FROM exploration_nodes WHERE exploration_id=$1`, s.expID).Scan(&running, &goals, &met)
	return
}
