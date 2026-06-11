-- Sticky inbox items are projected from workspace event history into
-- inbox_items. The legacy sticky_inbox_items table has no remaining readers
-- or writers; drop it so event history stays the only inbox source of truth.
DROP TABLE IF EXISTS sticky_inbox_items;
