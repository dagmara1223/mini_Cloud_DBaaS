import psycopg2

conn = psycopg2.connect(  # connecting with database. Pipeline: Python -> localhost -> Docker -> Postgres
    host="localhost",
    port=5433,
    database="cloud_metadata",
    user="postgres",
    password="admin"
)

cur = conn.cursor()

with open("db_manager/schema.sql", "r") as f: # Sending to Postgres contents of schema.sql. 
    cur.execute(f.read())

conn.commit()
cur.close()
conn.close()

print("DB initialized")