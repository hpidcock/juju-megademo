// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package triggers

import (
	"fmt"

	"github.com/juju/juju/core/database/schema"
)

// ChangeLogTriggersForDebugChangeStream generates the update trigger for
// the debug_change_stream table. Only the update trigger is needed because
// the table is a single-row singleton seeded at schema creation time.
func ChangeLogTriggersForDebugChangeStream(
	columnName string,
	namespaceID int,
) func() schema.Patch {
	return func() schema.Patch {
		return schema.MakePatch(fmt.Sprintf(`
-- insert namespace for DebugChangeStream
INSERT INTO change_log_namespace VALUES (%[2]d, 'debug_change_stream', 'DebugChangeStream changes based on %[1]s');

-- update trigger for DebugChangeStream
CREATE TRIGGER trg_log_debug_change_stream_update
AFTER UPDATE ON debug_change_stream FOR EACH ROW
WHEN NEW.state != OLD.state
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (2, %[2]d, OLD.%[1]s, DATETIME('now', 'utc'));
END;`, columnName, namespaceID))
	}
}
