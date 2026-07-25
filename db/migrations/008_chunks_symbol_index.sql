-- searchStructuralKnowledgeFTS runs a correlated subquery per matched symbol
-- (SELECT content FROM code_chunks WHERE symbol_id=? ...); without an index
-- on symbol_id that subquery does a full table scan of code_chunks for every
-- one of the ~96 candidate rows, which is the dominant cost of context
-- gathering on any project with a non-trivial number of indexed chunks.
CREATE INDEX IF NOT EXISTS idx_chunks_symbol ON code_chunks(symbol_id);
