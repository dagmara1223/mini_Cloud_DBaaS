CREATE TABLE nodes (     -- nodes represents servers. ex: id : 1, name: "node-1", ip: "192.168.1.10", status:"active"
    id SERIAL PRIMARY KEY,
    name TEXT,
    ip_address TEXT,
    status TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE databases ( -- client databases 
    id SERIAL PRIMARY KEY,
    name TEXT,
    node_id INT REFERENCES nodes(id),  --database must belong to a server
    status TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE containers ( -- node -> database -> container
    id SERIAL PRIMARY KEY,
    database_id INT REFERENCES databases(id),
    docker_id TEXT,
    port INT,
    status TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);