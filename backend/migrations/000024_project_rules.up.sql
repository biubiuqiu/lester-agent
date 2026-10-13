ALTER TABLE projects ADD COLUMN rules_content text NOT NULL DEFAULT '' CHECK(char_length(rules_content)<=20000);
ALTER TABLE projects ADD COLUMN rules_version bigint NOT NULL DEFAULT 1;
