
	    WITH page AS (
	        SELECT id, title, sub_id, sub_obj, created_at, update_at, deleted_at
	        FROM main
	        WHERE deleted_at IS NULL AND ($1::bigint IS NULL OR id = $1)
	        ORDER BY id ASC LIMIT $2 OFFSET $3
	    )
	    SELECT m.id, m.title, m.sub_id, m.sub_obj, m.created_at, m.update_at, m.deleted_at,
	        COALESCE(
	            (t.id IS NOT NULL)::int + (b.id IS NOT NULL)::int + (c.id IS NOT NULL)::int = 1
	            AND CASE m.sub_obj
	                WHEN 'tools' THEN t.id = m.sub_id AND t.deleted_at IS NULL
	                WHEN 'tables' THEN b.id = m.sub_id AND b.deleted_at IS NULL
	                WHEN 'chairs' THEN c.id = m.sub_id AND c.deleted_at IS NULL
	            END, false) AS valid,
	        COALESCE(t.id,b.id,c.id) AS satellite_id,
	        COALESCE(t.main_id,b.main_id,c.main_id) AS satellite_main_id,
	        COALESCE(t.created_at,b.created_at,c.created_at) AS satellite_created_at,
	        COALESCE(t.update_at,b.update_at,c.update_at) AS satellite_updated_at,
	        COALESCE(t.deleted_at,b.deleted_at,c.deleted_at) AS satellite_deleted_at,
	        COALESCE(t.description1,b.description2,c.description3) AS description,
	        c.type::text AS chair_type
	    FROM page m
	    LEFT JOIN tools t ON t.main_id = m.id
	    LEFT JOIN tables b ON b.main_id = m.id
	    LEFT JOIN chairs c ON c.main_id = m.id
	    ORDER BY m.id ASC;
