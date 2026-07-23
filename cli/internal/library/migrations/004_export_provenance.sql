ALTER TABLE export_files ADD COLUMN article_id TEXT REFERENCES articles(id) ON DELETE SET NULL;
ALTER TABLE export_files ADD COLUMN status TEXT NOT NULL DEFAULT 'written';

-- Prefer an exact output-path mapping from legacy provenance-compatible
-- manifests. This preserves affected article IDs for multi-file exports.
UPDATE export_files
SET article_id = (
  SELECT COALESCE(
    json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleId'),
    CASE
      WHEN json_type(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds') = 'array'
        AND json_array_length(json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds')) = 1
      THEN json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds[0]')
    END
  )
  FROM exports legacy_export
  JOIN json_each(
    CASE WHEN json_valid(legacy_export.manifest_json) THEN legacy_export.manifest_json ELSE '{}'
    END,
    '$.outputs'
  ) AS output
  WHERE legacy_export.id = export_files.export_id
    AND json_valid(legacy_export.manifest_json)
    AND output.type = 'object'
    AND json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.path') = export_files.relative_path
    AND COALESCE(
      json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleId'),
      CASE
        WHEN json_type(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds') = 'array'
          AND json_array_length(json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds')) = 1
        THEN json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds[0]')
      END
    ) IN (
      SELECT article.id FROM articles article
      WHERE article.profile_id = legacy_export.profile_id
    )
  GROUP BY legacy_export.id
  HAVING COUNT(DISTINCT COALESCE(
    json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleId'),
    CASE
      WHEN json_type(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds') = 'array'
        AND json_array_length(json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds')) = 1
      THEN json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds[0]')
    END
  )) = 1
)
WHERE article_id IS NULL
  AND EXISTS (
    SELECT 1
    FROM exports legacy_export
    JOIN json_each(
      CASE WHEN json_valid(legacy_export.manifest_json) THEN legacy_export.manifest_json ELSE '{}'
      END,
      '$.outputs'
    ) AS output
    WHERE legacy_export.id = export_files.export_id
      AND json_valid(legacy_export.manifest_json)
      AND output.type = 'object'
      AND json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.path') = export_files.relative_path
      AND COALESCE(
        json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleId'),
        CASE
          WHEN json_type(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds') = 'array'
            AND json_array_length(json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds')) = 1
          THEN json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds[0]')
        END
      ) IN (
        SELECT article.id FROM articles article
        WHERE article.profile_id = legacy_export.profile_id
      )
    GROUP BY legacy_export.id
    HAVING COUNT(DISTINCT COALESCE(
      json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleId'),
      CASE
        WHEN json_type(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds') = 'array'
          AND json_array_length(json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds')) = 1
        THEN json_extract(CASE WHEN output.type = 'object' THEN output.value ELSE '{}' END, '$.articleIds[0]')
      END
    )) = 1
  );

-- A single selected article can own multiple output files (HTML plus local
-- assets), so safely apply that one ID to every remaining file.
UPDATE export_files
SET article_id = (
  SELECT json_extract(
    CASE
      WHEN json_valid(legacy_export.manifest_json)
        AND json_type(legacy_export.manifest_json, '$.selection.articleIds') = 'array'
        THEN json_extract(legacy_export.manifest_json, '$.selection.articleIds')
      WHEN json_valid(legacy_export.manifest_json)
        AND json_type(legacy_export.manifest_json, '$.articleIds') = 'array'
        THEN json_extract(legacy_export.manifest_json, '$.articleIds')
      ELSE '[]'
    END,
    '$[0]'
  )
  FROM exports legacy_export
  WHERE legacy_export.id = export_files.export_id
    AND json_array_length(
      CASE
        WHEN json_valid(legacy_export.manifest_json)
          AND json_type(legacy_export.manifest_json, '$.selection.articleIds') = 'array'
          THEN json_extract(legacy_export.manifest_json, '$.selection.articleIds')
        WHEN json_valid(legacy_export.manifest_json)
          AND json_type(legacy_export.manifest_json, '$.articleIds') = 'array'
          THEN json_extract(legacy_export.manifest_json, '$.articleIds')
        ELSE '[]'
      END
    ) = 1
  LIMIT 1
)
WHERE article_id IS NULL
  AND (
    SELECT json_extract(
      CASE
        WHEN json_valid(legacy_export.manifest_json)
          AND json_type(legacy_export.manifest_json, '$.selection.articleIds') = 'array'
          THEN json_extract(legacy_export.manifest_json, '$.selection.articleIds')
        WHEN json_valid(legacy_export.manifest_json)
          AND json_type(legacy_export.manifest_json, '$.articleIds') = 'array'
          THEN json_extract(legacy_export.manifest_json, '$.articleIds')
        ELSE '[]'
      END,
      '$[0]'
    )
    FROM exports legacy_export
    WHERE legacy_export.id = export_files.export_id
  ) IN (
    SELECT article.id
    FROM articles article
    JOIN exports owner_export ON owner_export.id = export_files.export_id
    WHERE article.profile_id = owner_export.profile_id
  );
