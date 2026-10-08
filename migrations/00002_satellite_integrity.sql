-- +goose Up
LOCK TABLE main, tools, tables, chairs IN SHARE ROW EXCLUSIVE MODE;

-- +goose StatementBegin
CREATE FUNCTION check_main_satellite(owner_id BIGINT) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE
    owner main%ROWTYPE;
    satellite_count BIGINT;
    consistent BOOLEAN;
BEGIN
    SELECT * INTO owner FROM main WHERE id = owner_id FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT count(*), bool_and(
        satellite.kind = owner.sub_obj AND satellite.id = owner.sub_id
        AND satellite.deleted_at IS NOT DISTINCT FROM owner.deleted_at
    ) INTO satellite_count, consistent
    FROM (
        SELECT 'tools' AS kind, id, deleted_at FROM tools WHERE main_id = owner_id
        UNION ALL
        SELECT 'tables', id, deleted_at FROM tables WHERE main_id = owner_id
        UNION ALL
        SELECT 'chairs', id, deleted_at FROM chairs WHERE main_id = owner_id
    ) satellite;

    IF satellite_count <> 1 OR consistent IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'main % has an inconsistent satellite relationship', owner_id
            USING ERRCODE = '23514', CONSTRAINT = 'main_satellite_integrity';
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION enforce_main_satellite() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    owner_ids BIGINT[] := ARRAY[]::BIGINT[];
    owner_id BIGINT;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        IF TG_TABLE_NAME = 'main' THEN
            owner_ids := array_append(owner_ids, OLD.id);
        ELSE
            owner_ids := array_append(owner_ids, OLD.main_id);
        END IF;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        IF TG_TABLE_NAME = 'main' THEN
            owner_ids := array_append(owner_ids, NEW.id);
        ELSE
            owner_ids := array_append(owner_ids, NEW.main_id);
        END IF;
    END IF;

    FOR owner_id IN SELECT DISTINCT unnest(owner_ids) ORDER BY 1 LOOP
        PERFORM check_main_satellite(owner_id);
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER main_satellite_integrity
AFTER INSERT OR DELETE OR UPDATE OF id, sub_id, sub_obj, deleted_at ON main
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_main_satellite();

CREATE CONSTRAINT TRIGGER tools_satellite_integrity
AFTER INSERT OR DELETE OR UPDATE OF id, main_id, deleted_at ON tools
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_main_satellite();

CREATE CONSTRAINT TRIGGER tables_satellite_integrity
AFTER INSERT OR DELETE OR UPDATE OF id, main_id, deleted_at ON tables
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_main_satellite();

CREATE CONSTRAINT TRIGGER chairs_satellite_integrity
AFTER INSERT OR DELETE OR UPDATE OF id, main_id, deleted_at ON chairs
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_main_satellite();

-- +goose StatementBegin
DO $$
DECLARE
    owner_id BIGINT;
BEGIN
    FOR owner_id IN SELECT id FROM main ORDER BY id LOOP
        PERFORM check_main_satellite(owner_id);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER chairs_satellite_integrity ON chairs;
DROP TRIGGER tables_satellite_integrity ON tables;
DROP TRIGGER tools_satellite_integrity ON tools;
DROP TRIGGER main_satellite_integrity ON main;
DROP FUNCTION enforce_main_satellite();
DROP FUNCTION check_main_satellite(BIGINT);
