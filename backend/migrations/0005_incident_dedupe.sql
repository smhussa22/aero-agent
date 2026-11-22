-- alert events are delivered at least once; one incident per alert
CREATE UNIQUE INDEX incidents_alert_id ON incidents (alert_id);
