"""
Node fastapi agent odpowiada za: tworzenie, usuwanie baz, wykonywanie SQL, raport stanu
Każdy node uruchamia aplikację na porcie 8000.
Żeby działało, musi być uruchomiony docker desktop!
Następnie należy udać się na stronę: http://127.0.0.1:8000/docs 

Na potrzeby demo, aby zadziałała autentykacja, należy wprowadzić :  !!!!!!!!!!!
dev-secret-change-me 
gdy pojawi się okienko w Swaggerze.  (teraz powinno działać 
automatycznie bo mamy plik .env)
Aby wygenerować prawdziwy token - w terminalu: 

# 1. Wygeneruj klucz
python -c "import secrets; print(secrets.token_hex(32))"
# np. dostajemy: a1b2c3d4e5f6...

# 2. Uruchomienie z tym kluczem
NODE_API_KEY=a1b2c3d4e5f6... uvicorn node_agent:app --port 8000

# Na windows (chyba):
set NODE_API_KEY=a1b2c3d4e5f6...
uvicorn node_agent:app --port 8000

Architektura: 

------------------------------Zarządzanie bazami danych ----------------
POST /databases -> uruchamia kontener postgreSQL, zwraca db_id + port + connection string
GET /databases -> lista wszystkich baz hostowanych na tym node
GET /databases/{db_id} -> port, status, owner, uptime dla kontenera
DELETE /databases/{db_id} -> docker stop + rm, usuwa z local registry  

-------------------------------Life bazy ---------------------------
POST /databases/{db_id}/start -> docker start czyli wznawia zatrzymany kontener
POST /databases/{db_id}/stop -> docker stop zatrzymuje kontener, dane zachowanie

----------------------------------SQL---------------------------------
POST /databases/{db_id}/query -> laczy sie do lokalnego postgresql, wykonuje sql, zwraca rows

----------------------------------node management -----------------
GET /health -> status node, aktywne dbs ilosc
GET /metrics -> db_count, cpu_usage, memory_usage

"""
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import Optional
import docker
import psycopg2
import psutil
import uuid
import os

# token 
from dotenv import load_dotenv
load_dotenv()

# importy pod auth i persystencję 
import secrets
import json
from pathlib import Path
from fastapi import Depends, Security
from fastapi.security import APIKeyHeader
# ---- 
from fastapi.middleware.cors import CORSMiddleware


# autentykacja --------------------------------------------------
# Klucz wczytany ze zmiennej środowiskowej NODE_API_KEY
# Klucz wygenerowany, uruchomiony w terminalu:
#   python -c "import secrets; print(secrets.token_hex(32))"
# Następnie należy uruchomić agenta:
#   NODE_API_KEY=klucz uvicorn node_agent:app --port 8000
# Na potrzeby developmentu i testów na teraz zostaje domyślny klucz "dev-secret-change-me"

API_KEY = os.getenv("NODE_API_KEY", "dev-secret-change-me")
api_key_header = APIKeyHeader(name="X-API-Key")
 
def verify_key(key: str = Security(api_key_header)):
    """Sprawdzamy, czy klucz API w headerze X-API-Key jest poprawny."""
    if not secrets.compare_digest(key, API_KEY):
        raise HTTPException(status_code=401, detail="Invalid API Key")
    
# persystencja rejestru ------------------------------------------------
# db_registry zapisywany jest do pliku JSON przy każdej zmianie stanu.
# dzięki temu po restarcie agenta bazy nie znikają z rejestru (zapisany stan)
REGISTRY_FILE = Path("db_registry.json")

def _save_registry():
    """Zapisuje aktualny stan rejestru do pliku JSON."""
    with open(REGISTRY_FILE, "w") as f:
        json.dump(db_registry, f, indent=2)
 
def _load_registry() -> dict:
    """Wczytuje rejestr z pliku JSON przy starcie aplikacji."""
    if REGISTRY_FILE.exists():
        try:
            with open(REGISTRY_FILE, "r") as f:
                content = f.read().strip()
                if not content: # plik pusty
                    return {}
                return json.loads(content)
        except json.JSONDecodeError: # plik uszkodzony
            print("UWAGA: db_registry.json uszkodzony - zaczynam od pustego rejestru")
            return {}
    return {}

# dodanie persystencji - klucza
app = FastAPI(
    title="Node Agent",
    version="1.0.0",
    dependencies=[Depends(verify_key)]
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=[
        "http://localhost:3000",
        "http://localhost:3001"
    ],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Klient Docker i lokalny rejestr baz ----------------------------------
docker_client = docker.from_env()

# In-memory registry: db_id -> { db_name, port, owner, container_id, status }
db_registry: dict[str, dict] = _load_registry() # wczytujemy stan przy starcie

POSTGRES_IMAGE = "postgres:16-alpine"
PORT_START = 5500  # port 5500 zarezerwowany dla ewentualnego systemowego PG

# synchro z dockerem na starcie ---------------------------------------------
@app.on_event("startup")
def sync_registry_with_docker():
    """
    Przy starcie agenta sprawdza rzeczywisty stan każdego kontenera w Dockerze
    i aktualizuje status w rejestrze
    """
    for db_id, entry in db_registry.items():
        try:
            container = docker_client.containers.get(entry["container_id"])
            entry["status"] = container.status  # aktualizacja ze stanem z Dockera
        except docker.errors.NotFound:
            entry["status"] = "missing"  # kontener zniknął np. ręcznie został usunięty
    _save_registry()

# Pierwszy wolny port, który nie jest używany przez bazy
def _next_free_port() -> int:
    used = {v["port"] for v in db_registry.values()}
    port = PORT_START
    while port in used:
        port += 1
    return port

# Sprawdzenie, czy baza o danym db_id istnieje, jesli nie - zwraca 404 error
def _get_db_or_404(db_id: str) -> dict:
    if db_id not in db_registry:
        raise HTTPException(status_code=404, detail=f"DB {db_id!r} not found on this node")
    return db_registry[db_id]

#Schematy req response ----------------------------------------------
class CreateDBRequest(BaseModel):
    db_name: str
    owner: str
    password: str = "secret"


class QueryRequest(BaseModel):
    query: str
    params: Optional[list] = None

# create database POST/databases ------------------------------------
@app.post("/databases", status_code=201)
def create_database(req: CreateDBRequest):
    """
    Uruchamia kontener postgresql dla nowej bazy,
    zwraca db_id, host, port oraz gotowy connection string.
    """
    db_id = str(uuid.uuid4())[:8]
    port = _next_free_port()
    container_name = f"pg_{db_id}"

    try:
        container = docker_client.containers.run(
            POSTGRES_IMAGE,
            name=container_name,
            detach=True,
            environment={
                "POSTGRES_DB": req.db_name,
                "POSTGRES_USER": req.owner,
                "POSTGRES_PASSWORD": req.password,
            },
            ports={"5432/tcp": port},
        )
    except docker.errors.APIError as e:
        raise HTTPException(status_code=500, detail=f"Docker error: {e}")

    db_registry[db_id] = {
        "db_id": db_id,
        "db_name": req.db_name,
        "owner": req.owner,
        "password": req.password,
        "port": port,
        "container_id": container.id,
        "container_name": container_name,
        "status": "running",
    }
    _save_registry() # zapisanie stanu

    hostname = os.getenv("NODE_HOST", "localhost")
    return {
        "db_id": db_id,
        "host": hostname,
        "port": port,
        "db_name": req.db_name,
        "owner": req.owner,
        "status": "running",
        "connection_string": f"postgresql://{req.owner}:{req.password}@{hostname}:{port}/{req.db_name}",
    }

# -----------------------get databases --------
@app.get("/databases")
def list_databases():
    """
    Zwraca wszystkie bazy hostowane na tym node
    """
    return list(db_registry.values())

# ------------------------ get databases info-------
@app.get("/databases/{db_id}")
def get_database(db_id: str):
    """
    port, status, owner, container_id.
    """
    entry = _get_db_or_404(db_id)

    try:
        container = docker_client.containers.get(entry["container_id"])
        entry["status"] = container.status
    except docker.errors.NotFound:
        entry["status"] = "missing"

    return entry

# ------------------------------------------delete database------
@app.delete("/databases/{db_id}")
def delete_database(db_id: str):
    """
    stop i usuń kontener PostgreSQL. Dane przepadają (brak volume),
    czyści wpis z lokalnego rejestru
    """
    entry = _get_db_or_404(db_id)

    try:
        container = docker_client.containers.get(entry["container_id"])
        container.stop(timeout=5)
        container.remove()
    except docker.errors.NotFound:
        pass  # jesli nie istnieje i tak
    except docker.errors.APIError as e:
        raise HTTPException(status_code=500, detail=f"Docker error: {e}")

    del db_registry[db_id]
    _save_registry()
    return {"status": "deleted", "db_id": db_id}

# ---------------------- wznowienie zatrzymanego kontenera------
@app.post("/databases/{db_id}/start")
def start_database(db_id: str):
    """
    Uruchamia wcześniej zatrzymany kontener bez jego usuwania przy zachowaniu danych.
    """
    entry = _get_db_or_404(db_id)

    try:
        container = docker_client.containers.get(entry["container_id"])
        container.start()
        entry["status"] = "running"
    except docker.errors.NotFound:
        raise HTTPException(status_code=404, detail="Container not found")
    except docker.errors.APIError as e:
        raise HTTPException(status_code=500, detail=f"Docker error: {e}")
    
    _save_registry()

    return {"status": "running", "db_id": db_id, "port": entry["port"]}

# ------------------- zatrzymaj ale nie usuwaj ----------
@app.post("/databases/{db_id}/stop")
def stop_database(db_id: str):
    """
    Zatrzymuje kontener bez usuwania danych.
    """
    entry = _get_db_or_404(db_id)

    try:
        container = docker_client.containers.get(entry["container_id"])
        container.stop(timeout=5)
        entry["status"] = "stopped"
    except docker.errors.NotFound:
        raise HTTPException(status_code=404, detail="Container not found")
    except docker.errors.APIError as e:
        raise HTTPException(status_code=500, detail=f"Docker error: {e}")
    
    _save_registry()

    return {"status": "stopped", "db_id": db_id}


# ------------- EXECUTE SQL ------------------------
@app.post("/databases/{db_id}/query")
def execute_query(db_id: str, req: QueryRequest):
    """
    Łączy się do lokalnego PostgreSQL i wykonuje dowolne zapytanie SQL -> 
    Zwraca rows (dla SELECT) lub affected_rows (dla INSERT/UPDATE/DELETE).
    """
    entry = _get_db_or_404(db_id)

    if entry["status"] != "running":
        raise HTTPException(status_code=409, detail=f"DB is {entry['status']}, not running")

    conn = None
    try:
        conn = psycopg2.connect(
            host="localhost",
            port=entry["port"],
            dbname=entry["db_name"],
            user=entry["owner"],
            password=entry["password"],
            connect_timeout=5,
        )
        cur = conn.cursor()
        cur.execute(req.query, req.params or [])

        if cur.description:  #select
            columns = [d[0] for d in cur.description]
            rows = [dict(zip(columns, row)) for row in cur.fetchall()]
            conn.commit()
            return {"status": "ok", "rows": rows, "row_count": len(rows)}
        else:  # insert /update / delete 
            affected = cur.rowcount
            conn.commit()
            return {"status": "ok", "rows": [], "affected_rows": affected}

    except psycopg2.Error as e:
        raise HTTPException(status_code=400, detail=f"SQL error: {e}")
    finally:
        if conn:
            conn.close()

# ------- health check ------------------
@app.get("/health")
def health_check():
    """
    Sprawdza czy node żyje, gdzie active_dbs to liczba kontenerów ze statusem 'running'.
    """
    active = sum(1 for v in db_registry.values() if v.get("status") == "running")
    return {
        "status": "ok",
        "active_dbs": active,
        "total_dbs": len(db_registry),
    }

# ----------------- metryki -------------------------
@app.get("/metrics")
def get_metrics():
    """
    Metryki obciążenia node używane przez load balancer, szczególnie przydatne do implementacji 
    wyboru kontenera przez load balancer
    """
    active = sum(1 for v in db_registry.values() if v.get("status") == "running")
    return {
        "db_count": len(db_registry),
        "active_dbs": active,
        "cpu_percent": psutil.cpu_percent(interval=0.1),
        "mem_percent": psutil.virtual_memory().percent,
        "mem_available_mb": round(psutil.virtual_memory().available / 1024 / 1024),
    }

@app.get("/databases/{db_id}/metrics")
def db_metrics(db_id: str):
    entry = _get_db_or_404(db_id)

    try:
        container = docker_client.containers.get(entry["container_id"])
        stats = container.stats(stream=False)

        cpu_delta = stats["cpu_stats"]["cpu_usage"]["total_usage"] - \
                    stats["precpu_stats"]["cpu_usage"]["total_usage"]

        system_delta = stats["cpu_stats"]["system_cpu_usage"] - \
                       stats["precpu_stats"]["system_cpu_usage"]

        cpu_percent = 0.0
        if system_delta > 0:
            cpu_percent = (cpu_delta / system_delta) * len(stats["cpu_stats"]["cpu_usage"]["percpu_usage"]) * 100

        return {
            "cpu_percent": round(cpu_percent, 2)
        }

    except Exception as e:
        return {"cpu_percent": 0}
    
# ---------------------- tworzenie repliki ----------------------

@app.post("/databases/{db_id}/replica", status_code=201)
def create_replica(db_id: str):
    """
    Tworzy read-only replikę dla istniejącej bazy (primary).
    Wykorzystuje pg_basebackup + streaming replication.
    """
    primary = _get_db_or_404(db_id)

    if primary.get("role") == "replica":
        raise HTTPException(status_code=400, detail="Cannot replicate a replica")

    replica_id = str(uuid.uuid4())[:8]
    port = _next_free_port()
    container_name = f"pg_replica_{replica_id}"

    try:
        # uruchamiamy pusty kontener
        container = docker_client.containers.run(
            POSTGRES_IMAGE,
            name=container_name,
            detach=True,
            environment={
                "POSTGRES_PASSWORD": primary["password"],
            },
            ports={"5432/tcp": port},
        )

        # TODO: w realnym systemie:
        # - pg_basebackup z primary
        # - recovery.conf / standby.signal
        # - PRIMARY_CONNINFO

    except docker.errors.APIError as e:
        raise HTTPException(status_code=500, detail=f"Docker error: {e}")

    db_registry[replica_id] = {
        "db_id": replica_id,
        "db_name": primary["db_name"],
        "owner": primary["owner"],
        "password": primary["password"],
        "port": port,
        "container_id": container.id,
        "container_name": container_name,
        "status": "running",
        "role": "replica",
        "primary_db_id": db_id,
    }

    _save_registry()

    return {
        "replica_id": replica_id,
        "primary_db_id": db_id,
        "port": port,
        "status": "running",
        "role": "replica",
    }

#---------------------- pozwol na replikacje  ----------------------
@app.post("/databases/{db_id}/enable_replication")
def enable_replication(db_id: str):
    """
    Konfiguruje primary do replikacji:
    - wal_level=replica
    - max_wal_senders
    - tworzy usera replication
    """
    entry = _get_db_or_404(db_id)

    if entry.get("role") == "replica":
        raise HTTPException(status_code=400, detail="Replica cannot be primary")

    try:
        container = docker_client.containers.get(entry["container_id"])

        # ustawienia postgres 
        container.exec_run("echo \"wal_level=replica\" >> /var/lib/postgresql/data/postgresql.conf")
        container.exec_run("echo \"max_wal_senders=5\" >> /var/lib/postgresql/data/postgresql.conf")

        # user do replikacji
        container.exec_run(
            f'psql -U {entry["owner"]} -c "CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD \'replica_pass\';"'
        )

    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

    return {"status": "replication_enabled", "db_id": db_id}

# ---------------------- read only query ----------------------

@app.post("/databases/{db_id}/read_query")
def execute_read_query(db_id: str, req: QueryRequest):
    """
    Endpoint tylko dla SELECT (używany przez LB dla replik).
    """
    entry = _get_db_or_404(db_id)

    if entry.get("role") != "replica":
        raise HTTPException(status_code=400, detail="Not a replica")

    if not req.query.strip().lower().startswith("select"):
        raise HTTPException(status_code=400, detail="Replica is read-only")

    conn = None
    try:
        conn = psycopg2.connect(
            host="localhost",
            port=entry["port"],
            dbname=entry["db_name"],
            user=entry["owner"],
            password=entry["password"],
            connect_timeout=5,
        )

        cur = conn.cursor()
        cur.execute(req.query, req.params or [])

        columns = [d[0] for d in cur.description]
        rows = [dict(zip(columns, row)) for row in cur.fetchall()]

        return {"status": "ok", "rows": rows, "row_count": len(rows)}

    except psycopg2.Error as e:
        raise HTTPException(status_code=400, detail=f"SQL error: {e}")
    finally:
        if conn:
            conn.close()

#---------------------- lita replik ----------------------
@app.get("/databases/{db_id}/replicas")
def list_replicas(db_id: str):
    """
    Zwraca wszystkie repliki dla danej bazy primary.
    """
    _get_db_or_404(db_id)

    replicas = [
        v for v in db_registry.values()
        if v.get("primary_db_id") == db_id
    ]

    return replicas

#---------------------- promuj replikę----------------------
@app.post("/databases/{db_id}/promote")
def promote_replica(db_id: str):
    """
    Promuje replikę do primary (failover).
    """
    entry = _get_db_or_404(db_id)

    if entry.get("role") != "replica":
        raise HTTPException(status_code=400, detail="Not a replica")

    try:
        container = docker_client.containers.get(entry["container_id"])

        # postgres promote
        container.exec_run("pg_ctl promote -D /var/lib/postgresql/data")

        entry["role"] = "primary"
        entry["primary_db_id"] = None

    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

    _save_registry()

    return {"status": "promoted", "db_id": db_id}

# ---------------------- metryki replikacji ----------------------
@app.get("/databases/{db_id}/replication_status")
def replication_status(db_id: str):
    """
    Status replikacji (lag, rola, itp.)
    """
    entry = _get_db_or_404(db_id)

    conn = None
    try:
        conn = psycopg2.connect(
            host="localhost",
            port=entry["port"],
            dbname=entry["db_name"],
            user=entry["owner"],
            password=entry["password"],
        )

        cur = conn.cursor()

        # działa tylko dla primary
        cur.execute("""
            SELECT client_addr, state, sync_state
            FROM pg_stat_replication;
        """)

        rows = cur.fetchall()

        return {
            "role": entry.get("role", "primary"),
            "replicas": [
                {
                    "client_addr": r[0],
                    "state": r[1],
                    "sync_state": r[2],
                } for r in rows
            ]
        }

    except Exception:
        return {"role": entry.get("role"), "replicas": []}

    finally:
        if conn:
            conn.close()

# uruchomienie ------------------------
if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)