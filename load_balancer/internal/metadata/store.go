package metadata

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type DBRecord struct {
	DBID           string            `json:"db_id"`
	DBName		   string			 `json:"db_name"`
	PrimaryNodeID  string            `json:"primary_node_id"`
	ReplicaNodeIDs []string          `json:"replica_node_ids"`
	Status         string            `json:"status"`
	Owner          string            `json:"owner"`
	Role           string            `json:"role"`
	ReplicaMap     map[string]string `json:"replica_map"`
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	s := &Store{db: db}

	if err := s.initSchema(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS databases (
		db_id TEXT PRIMARY KEY,
		primary_node_id TEXT,
		replica_node_ids TEXT,
		replica_map TEXT,
		status TEXT,
		owner TEXT,
		role TEXT
	);
	`)
	return err
}

func (s *Store) Save(r DBRecord) error {
	if r.ReplicaNodeIDs == nil {
		r.ReplicaNodeIDs = []string{}
	}
	if r.ReplicaMap == nil {
		r.ReplicaMap = map[string]string{}
	}

	replicas, _ := json.Marshal(r.ReplicaNodeIDs)
	replicaMap, _ := json.Marshal(r.ReplicaMap)

	_, err := s.db.Exec(`
	INSERT INTO databases (
		db_id, primary_node_id, replica_node_ids, replica_map, status, owner, role
	)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(db_id) DO UPDATE SET
		primary_node_id=excluded.primary_node_id,
		replica_node_ids=excluded.replica_node_ids,
		replica_map=excluded.replica_map,
		status=excluded.status,
		owner=excluded.owner,
		role=excluded.role
	`,
		r.DBID,
		r.PrimaryNodeID,
		replicas,
		replicaMap,
		r.Status,
		r.Owner,
		r.Role,
	)

	return err
}

func (s *Store) scanRow(dbID, primary, replicas, rmap, status, owner, role string) (DBRecord, error) {
	r := DBRecord{
		DBID:          dbID,
		PrimaryNodeID: primary,
		Status:        status,
		Owner:         owner,
		Role:          role,
	}

	if replicas != "" {
		_ = json.Unmarshal([]byte(replicas), &r.ReplicaNodeIDs)
	}

	if rmap != "" {
		_ = json.Unmarshal([]byte(rmap), &r.ReplicaMap)
	}

	if r.ReplicaMap == nil {
		r.ReplicaMap = make(map[string]string)
	}

	if r.ReplicaNodeIDs == nil {
		r.ReplicaNodeIDs = []string{}
	}

	return r, nil
}

func (s *Store) Get(dbID string) (DBRecord, error) {
	var db, primary, replicas, rmap, status, owner, role string

	err := s.db.QueryRow(`
	SELECT db_id, primary_node_id, replica_node_ids, replica_map, status, owner, role
	FROM databases WHERE db_id = ?
	`, dbID).Scan(&db, &primary, &replicas, &rmap, &status, &owner, &role)

	if err != nil {
		return DBRecord{}, err
	}

	return s.scanRow(db, primary, replicas, rmap, status, owner, role)
}

func (s *Store) GetAll() ([]DBRecord, error) {
	rows, err := s.db.Query(`
	SELECT db_id, primary_node_id, replica_node_ids, replica_map, status, owner, role
	FROM databases
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBRecord

	for rows.Next() {
		var db, primary, replicas, rmap, status, owner, role string

		if err := rows.Scan(&db, &primary, &replicas, &rmap, &status, &owner, &role); err != nil {
			continue
		}

		r, _ := s.scanRow(db, primary, replicas, rmap, status, owner, role)
		out = append(out, r)
	}

	return out, nil
}

func (s *Store) GetDBsByNode(nodeURL string) ([]DBRecord, error) {
	rows, err := s.db.Query(`
	SELECT db_id, primary_node_id, replica_node_ids, replica_map, status, owner, role
	FROM databases
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBRecord

	for rows.Next() {
		var db, primary, replicas, rmap, status, owner, role string
		if err := rows.Scan(&db, &primary, &replicas, &rmap, &status, &owner, &role); err != nil {
			continue
		}

		r, _ := s.scanRow(db, primary, replicas, rmap, status, owner, role)

		if r.PrimaryNodeID == nodeURL || contains(r.ReplicaNodeIDs, nodeURL) {
			out = append(out, r)
		}
	}

	return out, nil
}

func (s *Store) AddReplica(dbID string, nodeURL string, replicaID string) error {
	rec, err := s.Get(dbID)
	if err != nil {
		return err
	}

	if rec.ReplicaMap == nil {
		rec.ReplicaMap = map[string]string{}
	}

	rec.ReplicaMap[nodeURL] = replicaID

	found := false
	for _, n := range rec.ReplicaNodeIDs {
		if n == nodeURL {
			found = true
			break
		}
	}

	if !found {
		rec.ReplicaNodeIDs = append(rec.ReplicaNodeIDs, nodeURL)
	}

	return s.Save(rec)
}

func (s *Store) UpdateStatus(dbID, status string) error {
	_, err := s.db.Exec(`UPDATE databases SET status = ? WHERE db_id = ?`, status, dbID)
	return err
}

func (s *Store) Delete(dbID string) error {
	res, err := s.db.Exec(`DELETE FROM databases WHERE db_id = ?`, dbID)
	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("not found")
	}

	return nil
}

func (s *Store) GetDBsByReplicaNode(nodeURL string) ([]DBRecord, error) {
	rows, err := s.db.Query(`
	SELECT db_id, primary_node_id, replica_node_ids, replica_map, status, owner, role
	FROM databases
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBRecord

	for rows.Next() {
		var db, primary, replicas, rmap, status, owner, role string
		if err := rows.Scan(&db, &primary, &replicas, &rmap, &status, &owner, &role); err != nil {
			continue
		}

		r, _ := s.scanRow(db, primary, replicas, rmap, status, owner, role)

		for _, n := range r.ReplicaNodeIDs {
			if n == nodeURL {
				out = append(out, r)
				break
			}
		}
	}

	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}