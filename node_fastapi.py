"""
Node fastapi agent odpowiada za: tworzenie, usuwanie baz, wykonywanie SQL, raport stanu
Każdy node uruchamia aplikację na porcie 8000.
Żeby działało, musi być uruchomiony docker desktop!
Następnie należy udać się na stronę: http://127.0.0.1:8000/docs 


Architektura: 

------------------------------Zarządzanie bazami danych ----------------
POST /databases -> uruchamia kontener postgreSQL, zwraca db_id + port + connection string
GET /databases -> lista wszystkich baz hostowanych na tym node
GET /databases/{db_id} -> port, status, owner, uptime dla kontenera
DELETE /databases/{db_id} -> docker stop + rm, usuwa z local registry   <----------NA RAZIE TYLE 

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

app = FastAPI(title="Node Agent", version="1.0.0")

# Klient Docker i lokalny rejestr baz ----------------------------------
docker_client = docker.from_env()

# In-memory registry: db_id -> { db_name, port, owner, container_id, status }
db_registry: dict[str, dict] = {}

POSTGRES_IMAGE = "postgres:16-alpine"
PORT_START = 5433  # port 5432 zarezerwowany dla ewentualnego systemowego PG

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
    return {"status": "deleted", "db_id": db_id}




# uruchomienie ------------------------
if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)