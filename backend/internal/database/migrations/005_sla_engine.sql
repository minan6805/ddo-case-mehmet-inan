CREATE TABLE sla_calendar (
    id integer PRIMARY KEY CHECK (id=1),
    timezone text NOT NULL,
    start_minute integer NOT NULL CHECK (start_minute BETWEEN 0 AND 1439),
    end_minute integer NOT NULL CHECK (end_minute BETWEEN 1 AND 1440),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    CHECK (start_minute<end_minute)
);
INSERT INTO sla_calendar(id,timezone,start_minute,end_minute) VALUES(1,'Europe/Istanbul',510,1080);
CREATE TABLE sla_holidays (
    day date PRIMARY KEY,
    closed_from_minute integer NOT NULL DEFAULT 0 CHECK (closed_from_minute BETWEEN 0 AND 1439),
    label text NOT NULL CHECK (length(trim(label)) BETWEEN 1 AND 120)
);
-- 2026 official holiday dates: https://www.borsaistanbul.com/resmi-tatil-gunleri
-- 0 closes the whole day; 780 closes the day from 13:00.
INSERT INTO sla_holidays(day,closed_from_minute,label) VALUES
('2026-01-01',0,'Yılbaşı'),('2026-03-19',780,'Ramazan Bayramı arifesi'),
('2026-03-20',0,'Ramazan Bayramı'),('2026-03-21',0,'Ramazan Bayramı'),('2026-03-22',0,'Ramazan Bayramı'),
('2026-04-23',0,'Ulusal Egemenlik ve Çocuk Bayramı'),('2026-05-01',0,'Emek ve Dayanışma Günü'),
('2026-05-19',0,'Atatürk’ü Anma Gençlik ve Spor Bayramı'),('2026-05-26',780,'Kurban Bayramı arifesi'),
('2026-05-27',0,'Kurban Bayramı'),('2026-05-28',0,'Kurban Bayramı'),('2026-05-29',0,'Kurban Bayramı'),('2026-05-30',0,'Kurban Bayramı'),
('2026-07-15',0,'Demokrasi ve Millî Birlik Günü'),('2026-08-30',0,'Zafer Bayramı'),
('2026-10-28',780,'Cumhuriyet Bayramı arifesi'),('2026-10-29',0,'Cumhuriyet Bayramı');
CREATE TABLE sla_policies (
    category_code text NOT NULL REFERENCES ticket_categories(code),
    priority text NOT NULL CHECK (priority IN ('low','normal','high','critical')),
    response_minutes integer NOT NULL CHECK (response_minutes BETWEEN 1 AND 43200),
    resolution_minutes integer NOT NULL CHECK (resolution_minutes BETWEEN 1 AND 43200),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    PRIMARY KEY(category_code,priority)
);
INSERT INTO sla_policies(category_code,priority,response_minutes,resolution_minutes)
SELECT c.code,p.priority,
    CASE WHEN c.code='network' THEN p.response_minutes/2 ELSE p.response_minutes END,
    CASE WHEN c.code='hardware' THEN p.resolution_minutes*2 WHEN c.code='network' THEN p.resolution_minutes/2 ELSE p.resolution_minutes END
FROM ticket_categories c CROSS JOIN (VALUES
    ('low',480,1920),('normal',240,960),('high',60,480),('critical',30,240)
) AS p(priority,response_minutes,resolution_minutes);

CREATE TABLE ticket_status_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id bigint NOT NULL REFERENCES tickets(id),
    status text NOT NULL CHECK (status IN ('open','in_progress','waiting_customer','resolved','closed')),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX ticket_status_history_ticket_time_idx ON ticket_status_history(ticket_id,occurred_at,id);
INSERT INTO ticket_status_history(ticket_id,status,occurred_at)
SELECT id,'open',created_at FROM tickets ORDER BY id;
INSERT INTO ticket_status_history(ticket_id,status,occurred_at)
SELECT entity_id,new_value->>'status',created_at FROM audit_log
WHERE entity_type='ticket' AND action='ticket.status' ORDER BY id;

CREATE TABLE ticket_sla (
    ticket_id bigint PRIMARY KEY REFERENCES tickets(id),
    response_budget_seconds bigint NOT NULL CHECK (response_budget_seconds>0),
    resolution_budget_seconds bigint NOT NULL CHECK (resolution_budget_seconds>0),
    policy_version bigint NOT NULL,
    calendar_version bigint NOT NULL,
    response_elapsed_seconds bigint NOT NULL DEFAULT 0 CHECK (response_elapsed_seconds>=0),
    resolution_elapsed_seconds bigint NOT NULL DEFAULT 0 CHECK (resolution_elapsed_seconds>=0),
    response_due_at timestamptz,
    resolution_due_at timestamptz,
    first_response_at timestamptz,
    completed_at timestamptz,
    paused boolean NOT NULL DEFAULT false,
    checked_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO ticket_sla(ticket_id,response_budget_seconds,resolution_budget_seconds,policy_version,calendar_version)
SELECT t.id,p.response_minutes*60,p.resolution_minutes*60,p.version,1
FROM tickets t JOIN sla_policies p ON p.category_code=t.category_code AND p.priority=t.priority;
CREATE INDEX ticket_sla_checked_idx ON ticket_sla(checked_at,ticket_id);

ALTER TABLE tickets ADD COLUMN escalation_level integer NOT NULL DEFAULT 0 CHECK (escalation_level>=0);
CREATE TABLE sla_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id bigint NOT NULL REFERENCES tickets(id),
    timer text NOT NULL CHECK (timer IN ('response','resolution')),
    stage text NOT NULL CHECK (stage IN ('warning','breach','escalation')),
    recipient_role text NOT NULL DEFAULT 'team_lead' REFERENCES roles(code),
    elapsed_seconds bigint NOT NULL,
    budget_seconds bigint NOT NULL,
    calendar_version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE(ticket_id,timer,stage)
);
CREATE INDEX sla_events_recent_idx ON sla_events(id DESC);
