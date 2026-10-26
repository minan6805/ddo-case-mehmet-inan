"""Compare two production indexes on disposable PostgreSQL and synthetic data."""
import json
from pathlib import Path
import re
import statistics
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ['docker', 'compose', '-p', 'destekmasa-index-proof', '-f', str(ROOT / 'compose.test.yaml')]


def sql(statement):
    result = subprocess.run(
        COMPOSE + ['exec', '-T', 'test-db', 'psql', '-X', '-qAt',
                   '-U', 'test_runner', '-d', 'destekmasa_test', '-v', 'ON_ERROR_STOP=1'],
        input=statement, text=True, capture_output=True, check=True,
    )
    return result.stdout.strip()


def measure(query):
    # Use natural plans with all other indexes retained. Warm each case equally.
    for _ in range(3):
        sql('EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) ' + query)
    samples = []
    plans = []
    for _ in range(7):
        plan = json.loads(sql('EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) ' + query))[0]
        samples.append(plan['Execution Time'])
        plans.append(plan)
    median = statistics.median(samples)
    return {'median_execution_ms': median, 'samples_ms': samples,
            'explain': plans[samples.index(median)]}


def main():
    # This project name and tmpfs database are dedicated to this measurement.
    subprocess.run(COMPOSE + ['up', '-d', '--wait', 'test-db'], cwd=ROOT, check=True, stdout=sys.stderr)
    try:
        # A stale interrupted benchmark must not silently reuse its data.
        if sql("SELECT count(*) FROM information_schema.tables WHERE table_schema='public'") != '0':
            raise RuntimeError('Measurement database is not empty; refusing to modify it.')
        migrations = sorted((ROOT / 'backend/internal/database/migrations').glob('*.sql'))
        sql('BEGIN;\n' + '\n'.join(path.read_text() for path in migrations) + '\nCOMMIT;')
        sql("""
INSERT INTO users(email,display_name,password_hash,role_code)
SELECT 'index-proof-' || n || '@example.invalid', 'Synthetic requester ' || n,
       'not-a-login-hash', 'employee' FROM generate_series(1,1000) n;
INSERT INTO tickets(requester_id,title,description,status,priority,category_code,created_at,updated_at)
SELECT 1 + n % 1000, 'Synthetic ticket ' || n, repeat('Synthetic description. ',12),
       (ARRAY['open','in_progress','waiting_customer','resolved','closed'])[1+n%5],
       (ARRAY['low','normal','high','critical'])[1+n%4],
       (ARRAY['general','network','hardware','software'])[1+n%4],
       TIMESTAMPTZ '2026-01-01 00:00:00+03' + (n % 365) * INTERVAL '1 day' + (n / 365) * INTERVAL '1 minute',
       TIMESTAMPTZ '2026-01-01 00:00:00+03' + (n % 365) * INTERVAL '1 day'
FROM generate_series(1,200000) n;
VACUUM (ANALYZE) tickets;
ANALYZE users;
""")
        source = (ROOT / 'backend/internal/tickets/tickets.go').read_text()
        select = re.search(r'const selectTicket = `([^`]+)`', source).group(1)
        listing = re.search(r'selectTicket\+`(WHERE \(\$1::boolean[^`]+)`', source).group(1)
        # Substitute the parameter values of a regular employee's second page.
        params = {1: 'false', 2: '42', 3: '180000', 4: "''", 5: '21', 6: "'all'"}
        listing = re.sub(r'\$(\d+)', lambda match: params[int(match[1])], listing)
        ticket_query = select + listing
        report_source = (ROOT / 'backend/internal/reports/service.go').read_text()
        cohort = re.search(r'const cohort = `([^`]+)`', report_source).group(1)
        summary = re.search(r'cohort\+`(SELECT count\(\*\), count\(\*\) FILTER[^`]+)`', report_source).group(1)
        report_params = {1: "TIMESTAMPTZ '2026-10-01 00:00:00+03'", 2: "TIMESTAMPTZ '2026-10-02 00:00:00+03'", 3: "'network'"}
        report_query = re.sub(r'\$(\d+)', lambda match: report_params[int(match[1])], cohort + summary)
        cases = [
            ('tickets_requester_id_desc_idx', 'CREATE INDEX tickets_requester_id_desc_idx ON tickets(requester_id,id DESC)', ticket_query),
            ('tickets_created_category_idx', 'CREATE INDEX tickets_created_category_idx ON tickets(created_at,category_code)', report_query),
        ]
        results = {'postgres_version': sql('SHOW server_version'), 'rows': 200000,
                   'requesters': 1000, 'warmups': 3, 'samples': 7,
                   'scope': 'Synthetic data; real migrations and production SELECTs with literal parameters. Other indexes remain enabled. Warm cache; not a cold-start or concurrency benchmark.',
                   'cases': []}
        for name, create, query in cases:
            indexed = measure(query)
            sql('DROP INDEX ' + name)
            try:
                unindexed = measure(query)
            finally:
                sql(create)
            results['cases'].append({'index': name, 'query': query,
                                     'with_index': indexed, 'without_index': unindexed})
        print(json.dumps(results, indent=2, ensure_ascii=False))
    finally:
        subprocess.run(COMPOSE + ['down', '--volumes'], cwd=ROOT, check=True, stdout=sys.stderr)


if __name__ == '__main__':
    main()
