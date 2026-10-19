-- +goose Up
-- +goose StatementBegin
-- A profile becomes three prompts (need, exclude, scoring) plus an ordered list of added
-- prompts. The previous document is kept under "legacy" so this can be reversed; a profile
-- created after the migration has no legacy key, and reversing does not rebuild it.
CREATE OR REPLACE FUNCTION growth_profile_prompts(p jsonb) RETURNS jsonb
LANGUAGE plpgsql AS $$
DECLARE
  line text;
  need text := '';
  excl text := '';
  scoring text := '';
  dims text := '';
  recent int := 0;
  qual int := 7;
  watch int := 4;
  pen int := 3;
  sc jsonb := COALESCE(p->'scoring', '{}'::jsonb);
BEGIN
  IF p ? 'need' OR jsonb_typeof(p->'scoring') = 'string' THEN
    RETURN p;
  END IF;

  IF btrim(COALESCE(p->>'name', '')) <> '' THEN
    need := need || 'Business: ' || btrim(p->>'name') || E'\n';
  END IF;
  IF btrim(COALESCE(p->>'pitch', '')) <> '' THEN
    need := need || 'Offer: ' || btrim(p->>'pitch') || E'\n';
  END IF;
  SELECT string_agg(btrim(x), ', ') INTO line
    FROM jsonb_array_elements_text(COALESCE(p->'games', '[]'::jsonb)) AS x
    WHERE btrim(x) <> '';
  IF line IS NOT NULL THEN need := need || 'Products: ' || line || E'\n'; END IF;
  IF btrim(COALESCE(p->>'genre', '')) <> '' THEN
    need := need || 'Category: ' || btrim(p->>'genre') || E'\n';
  END IF;
  SELECT string_agg(btrim(x), ', ') INTO line
    FROM jsonb_array_elements_text(COALESCE(p->'regions', '[]'::jsonb)) AS x WHERE btrim(x) <> '';
  IF line IS NOT NULL THEN need := need || 'Regions: ' || line || E'\n'; END IF;
  SELECT string_agg(btrim(x), ', ') INTO line
    FROM jsonb_array_elements_text(COALESCE(p->'languages', '[]'::jsonb)) AS x WHERE btrim(x) <> '';
  IF line IS NOT NULL THEN need := need || 'Languages: ' || line || E'\n'; END IF;
  IF COALESCE((p->>'default_target')::int, 0) > 0 THEN
    need := need || 'Lead target: ' || (p->>'default_target') || E'\n';
  END IF;
  SELECT string_agg(btrim(x), '; ') INTO line
    FROM jsonb_array_elements_text(COALESCE(p->'signals', '[]'::jsonb)) AS x WHERE btrim(x) <> '';
  IF line IS NOT NULL THEN need := need || 'Signal words: ' || line || E'\n'; END IF;
  SELECT string_agg(btrim(x), ', ') INTO line
    FROM jsonb_array_elements_text(COALESCE(p->'contact_types', '[]'::jsonb)) AS x
    WHERE btrim(x) <> '' AND lower(btrim(x)) <> 'platform_handle';
  IF line IS NOT NULL THEN need := need || 'Contacts to find: ' || line || E'\n'; END IF;
  IF btrim(COALESCE(p->>'notes', '')) <> '' THEN
    need := need || 'Notes: ' || regexp_replace(btrim(p->>'notes'), E'[[:space:]]+', ' ', 'g') || E'\n';
  END IF;

  SELECT string_agg('Competitor: ' || btrim(x), E'\n') INTO line
    FROM jsonb_array_elements_text(COALESCE(p->'competitors', '[]'::jsonb)) AS x WHERE btrim(x) <> '';
  IF line IS NOT NULL THEN excl := line; END IF;
  FOR line IN
    SELECT btrim(x) FROM jsonb_array_elements_text(COALESCE(p->'unwanted', p->'excludes', '[]'::jsonb)) AS x
  LOOP
    IF line = '' OR line ILIKE '%under 18%' OR line ILIKE '%underage%' OR line ILIKE '%minor%'
       OR line ILIKE '%stop gambling%' OR line ILIKE '%self-exclu%' OR line LIKE '%未成年%' OR line LIKE '%戒赌%' THEN
      CONTINUE;
    END IF;
    excl := excl || CASE WHEN excl = '' THEN '' ELSE E'\n' END || line;
  END LOOP;

  IF jsonb_typeof(sc->'dimensions') = 'array' AND jsonb_array_length(sc->'dimensions') > 0 THEN
    SELECT string_agg(format(E'%s = %s | %s | %s',
        COALESCE(NULLIF(btrim(d->>'key'), ''), NULLIF(btrim(d->>'name'), ''), 'signal'),
        COALESCE(d->>'points', '0'),
        COALESCE(NULLIF(btrim(d->>'name'), ''), NULLIF(btrim(d->>'key'), ''), 'signal'),
        COALESCE(d->>'description', '')), E'\n')
      INTO dims
      FROM jsonb_array_elements(sc->'dimensions') AS d
      WHERE btrim(COALESCE(d->>'name', '')) <> '' OR btrim(COALESCE(d->>'description', '')) <> '';
  ELSIF sc ? 'weights' THEN
    dims := format(E'player = %s | Audience | The sender uses or follows this kind of product or service themselves.', COALESCE(sc#>>'{weights,player}', '3')) || E'\n' ||
            format(E'pain = %s | Pain or switching | The sender is unhappy with, bored of or leaving what they use now, or asks for an alternative.', COALESCE(sc#>>'{weights,pain}', '3')) || E'\n' ||
            format(E'paying = %s | Paying | The sender mentions paying for this kind of product: purchases, deposits, top-ups, subscriptions.', COALESCE(sc#>>'{weights,paying}', '2')) || E'\n' ||
            format(E'match = %s | Fit | What the sender wants or uses fits the offer: its category, platform, region and style.', COALESCE(sc#>>'{weights,match}', '3'));
    recent := COALESCE((sc#>>'{weights,active}')::int, 0);
  ELSE
    dims := E'player = 3 | Audience | The sender uses or follows this kind of product or service themselves.\n' ||
            E'pain = 3 | Pain or switching | The sender is unhappy with, bored of or leaving what they use now, or asks for an alternative.\n' ||
            E'paying = 2 | Paying | The sender mentions paying for this kind of product: purchases, deposits, top-ups, subscriptions.\n' ||
            E'match = 3 | Fit | What the sender wants or uses fits the offer: its category, platform, region and style.';
    recent := 2;
  END IF;
  IF sc ? 'recent' THEN
    recent := COALESCE((sc#>>'{recent,points}')::int, recent);
  END IF;
  IF sc ? 'qualified' THEN qual := (sc->>'qualified')::int; END IF;
  IF sc ? 'watchlist' THEN watch := (sc->>'watchlist')::int; END IF;
  IF COALESCE((p->>'unwanted_penalty')::int, 0) > 0 THEN
    pen := (p->>'unwanted_penalty')::int;
  END IF;
  scoring := dims;
  IF recent > 0 THEN scoring := scoring || E'\nrecent = ' || recent::text; END IF;
  scoring := scoring || format(E'\nqualified >= %s\nwatchlist >= %s\nunwanted = -%s', qual, watch, pen);

  RETURN jsonb_build_object(
    'name', COALESCE(p->>'name', ''),
    'need', btrim(need),
    'exclude', btrim(excl),
    'scoring', btrim(scoring),
    'sections', '[]'::jsonb,
    'legacy', p
  );
END $$;

UPDATE growth_profiles SET profile = growth_profile_prompts(profile);
UPDATE growth_profile_drafts SET profile = growth_profile_prompts(profile);
DROP FUNCTION growth_profile_prompts(jsonb);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE growth_profiles SET profile = profile->'legacy'
  WHERE jsonb_typeof(profile->'legacy') = 'object';
UPDATE growth_profile_drafts SET profile = profile->'legacy'
  WHERE jsonb_typeof(profile->'legacy') = 'object';
-- +goose StatementEnd
