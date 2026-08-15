INSERT INTO crm.clients (username, full_name, city, contract_number) VALUES
    ('prothetic1',   'Иванов Иван Иванович',      'Москва',          'BP-2024-0001'),
    ('prothetic2',   'Петрова Мария Сергеевна',   'Санкт-Петербург', 'BP-2024-0002'),
    ('prothetic3',   'Сидоров Алексей Павлович',  'Казань',          'BP-2024-0003'),
    ('john.doe',     'John Doe',                  'Belgrade',        'BP-2024-0101'),
    ('alex.johnson', 'Alex Johnson',              'Belgrade',        'BP-2024-0102');

INSERT INTO crm.prostheses (prosthesis_serial, client_id, model, manufactured_at, warranty_until)
SELECT
    'BP-ARM-' || lpad(c.client_id::text, 4, '0'),
    c.client_id,
    CASE WHEN c.client_id % 2 = 0 THEN 'BionicARM Pro' ELSE 'BionicARM Lite' END,
    current_date - interval '400 days',
    current_date + interval '330 days'
FROM crm.clients c;

INSERT INTO crm.prostheses (prosthesis_serial, client_id, model, manufactured_at, warranty_until)
SELECT
    'BP-LEG-' || lpad(c.client_id::text, 4, '0'),
    c.client_id,
    'BionicLEG Pro',
    current_date - interval '200 days',
    current_date + interval '530 days'
FROM crm.clients c
WHERE c.username = 'prothetic1';

INSERT INTO telemetry.readings (
    prosthesis_serial, recorded_at, gesture, recognized, latency_ms, battery_level, signal_quality, error_code
)
SELECT
    p.prosthesis_serial,
    ts,
    (ARRAY['grip', 'pinch', 'release', 'rotate', 'point'])[1 + floor(random() * 5)::int],
    random() > 0.08,
    (55 + floor(random() * 90))::int,
    round((20 + random() * 80)::numeric, 2),
    round((60 + random() * 40)::numeric, 2),
    CASE WHEN random() < 0.03 THEN 'E' || (100 + floor(random() * 5))::text END
FROM crm.prostheses p
CROSS JOIN generate_series(
    date_trunc('day', now()) - interval '45 days',
    date_trunc('day', now()) + interval '18 hours',
    interval '37 minutes'
) AS ts;
