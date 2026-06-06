package metadata

import (
	"database/sql"
	"encoding/json"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type DBRecord struct {
    DBID           string   `json:"db_id"`
    PrimaryNodeID  string   `json:"primary_node_id"`
    ReplicaNodeIDs []string `json:"replica_node_ids"`
    Status         string   `json:"status"`
    Owner          string   `json:"owner"`
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
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
		status TEXT,
		owner TEXT
	);
	`)
	return err
}

func (s *Store) Save(record DBRecord) error {
	replicas, _ := json.Marshal(record.ReplicaNodeIDs)

	_, err := s.db.Exec(`
	INSERT INTO databases (db_id, primary_node_id, replica_node_ids, status, owner)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(db_id) DO UPDATE SET
		primary_node_id=excluded.primary_node_id,
		replica_node_ids=excluded.replica_node_ids,
		status=excluded.status,
		owner=excluded.owner
	`,
		record.DBID,
		record.PrimaryNodeID,
		replicas,
		record.Status,
		record.Owner,
	)

	return err
}

func (s *Store) Get(dbID string) (DBRecord, error) {
	var r DBRecord
	var replicas []byte

	err := s.db.QueryRow(`
	SELECT db_id, primary_node_id, replica_node_ids, status, owner
	FROM databases WHERE db_id = ?`,
		dbID,
	).Scan(&r.DBID, &r.PrimaryNodeID, &replicas, &r.Status, &r.Owner)

	if err != nil {
		return r, err
	}

	if len(replicas) > 0 {
		json.Unmarshal(replicas, &r.ReplicaNodeIDs)
	}

	return r, nil
}

func (s *Store) GetDBsByNode(nodeURL string) ([]DBRecord, error) {
	rows, err := s.db.Query(`
	SELECT db_id, primary_node_id, replica_node_ids, status, owner
	FROM databases WHERE primary_node_id = ?
	`, nodeURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DBRecord

	for rows.Next() {
		var r DBRecord
		var replicas []byte

		err := rows.Scan(&r.DBID, &r.PrimaryNodeID, &replicas, &r.Status, &r.Owner)
		if err != nil {
			continue
		}

		if len(replicas) > 0 {
			json.Unmarshal(replicas, &r.ReplicaNodeIDs)
		}

		result = append(result, r)
	}

	return result, nil
}

func (s *Store) AddReplica(dbID string, nodeURL string) error {
	record, err := s.Get(dbID)
	if err != nil {
		return err
	}

	for _, r := range record.ReplicaNodeIDs {
		if r == nodeURL {
			return nil
		}
	}

	record.ReplicaNodeIDs = append(record.ReplicaNodeIDs, nodeURL)

	return s.Save(record)
}

func (s *Store) GetAll() ([]DBRecord, error) {
	rows, err := s.db.Query(`
	SELECT db_id, primary_node_id, replica_node_ids, status, owner
	FROM databases
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DBRecord

	for rows.Next() {
		var r DBRecord
		var replicas []byte

		if err := rows.Scan(&r.DBID, &r.PrimaryNodeID, &replicas, &r.Status, &r.Owner); err != nil {
			continue
		}

		if len(replicas) > 0 {
			_ = json.Unmarshal(replicas, &r.ReplicaNodeIDs)
		}

		result = append(result, r)
	}

	return result, nil
}

func (s *Store) UpdateStatus(dbID, status string) (sql.Result, error) {
    return s.db.Exec(`
        UPDATE databases
        SET status = ?
        WHERE db_id = ?
    `, status, dbID)
}