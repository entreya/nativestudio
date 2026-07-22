CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
    workspace_id UNINDEXED,
    kind UNINDEXED,
    entity_id UNINDEXED,
    path,
    symbol,
    signature,
    content,
    tokenize='trigram'
);

INSERT INTO knowledge_fts(workspace_id,kind,entity_id,path,symbol,signature,content)
SELECT workspace_id,'symbol',id,path,symbol_name,signature,summary
FROM symbols WHERE status='active';

INSERT INTO knowledge_fts(workspace_id,kind,entity_id,path,symbol,signature,content)
SELECT workspace_id,'chunk',id,path,'','',content
FROM code_chunks WHERE status='active';
