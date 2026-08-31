ALTER TABLE users
  ADD COLUMN avatar_key text NOT NULL DEFAULT 'forest'
  CHECK (avatar_key IN ('forest', 'ocean', 'clay', 'lilac', 'amber', 'graphite'));
