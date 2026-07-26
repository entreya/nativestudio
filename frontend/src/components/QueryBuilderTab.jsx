import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Select, Button, Input, InputNumber, Table, Typography, Alert, Space, Empty, Divider, Tooltip, Dropdown } from 'antd';
import { PlusOutlined, DeleteOutlined, PlayCircleOutlined, ApartmentOutlined, SelectOutlined, ThunderboltOutlined, StopOutlined } from '@ant-design/icons';

const { Text, Title } = Typography;

let nextRowId = 1;
const newRowId = () => nextRowId++;

const JOIN_TYPES = [
  { value: 'INNER', label: 'INNER JOIN' },
  { value: 'LEFT', label: 'LEFT JOIN' },
];

const AGGREGATES = [
  { value: '', label: 'no aggregate' },
  { value: 'COUNT', label: 'COUNT' },
  { value: 'SUM', label: 'SUM' },
  { value: 'AVG', label: 'AVG' },
  { value: 'MIN', label: 'MIN' },
  { value: 'MAX', label: 'MAX' },
];

// Mirrors handlers/database.go's whereOperators — value is what the UI
// sends as "operator"; the backend is the actual authority on which
// operators are valid and what SQL they produce, this list only drives
// which one the dropdown shows next.
const OPERATORS = [
  { value: '=', label: '= equals' },
  { value: '!=', label: '≠ not equals' },
  { value: '<', label: '< less than' },
  { value: '<=', label: '≤ less or equal' },
  { value: '>', label: '> greater than' },
  { value: '>=', label: '≥ greater or equal' },
  { value: 'contains', label: 'contains' },
  { value: 'starts_with', label: 'starts with' },
  { value: 'ends_with', label: 'ends with' },
  { value: 'is_null', label: 'is empty' },
  { value: 'is_not_null', label: 'is not empty' },
];
const NO_VALUE_OPERATORS = new Set(['is_null', 'is_not_null']);

// Short glyph shown on the operator's own button (the reference this was
// modeled on shows "=", "≠", "<" etc. as the trigger itself, with the full
// wording only inside the dropdown menu).
const OPERATOR_GLYPHS = {
  '=': '=', '!=': '≠', '<': '<', '<=': '≤', '>': '>', '>=': '≥',
  contains: '⊃', starts_with: '⇥', ends_with: '↦', is_null: '∅', is_not_null: '∄',
};

const FIELD_SEP = '::';

// One flat, grouped list — each reachable table as a group header, its
// columns as selectable options underneath — so picking "which table, which
// column" is one dropdown instead of two, the way a real field picker reads.
function fieldOptions(reachableTables, columnsFor) {
  return reachableTables.map(table => ({
    label: table,
    title: table,
    options: columnsFor(table).map(col => ({ value: `${table}${FIELD_SEP}${col}`, label: col })),
  }));
}

function quoteIdent(name) { return '"' + name + '"'; }
function quoteLiteral(value) { return "'" + String(value ?? '').replaceAll("'", "''") + "'"; }

// Builds a readable (not necessarily byte-identical to the server's) preview
// of the SQL this configuration would produce, purely so the user can see
// what they're about to run as they build it. The backend rebuilds and
// validates its own version from the same structured fields when the query
// actually runs — this preview is never executed, so it doesn't need the
// server's identifier/schema validation to be safe, only to be readable.
function buildPreviewSQL({ fromTable, joins, selects, wheres, groupBy, limit }) {
  if (!fromTable) return '';
  const selectSql = selects.length === 0
    ? '*'
    : selects.map(s => {
        if (!s.table || !s.column) return '…';
        const ref = `${quoteIdent(s.table)}.${quoteIdent(s.column)}`;
        const wrapped = s.aggregate ? `${s.aggregate}(${ref})` : ref;
        const alias = s.alias || (s.aggregate ? `${s.aggregate.toLowerCase()}_${s.column}` : (joins.length > 0 ? `${s.table}.${s.column}` : ''));
        return alias ? `${wrapped} AS ${quoteIdent(alias)}` : wrapped;
      }).join(', ');

  let sql = `SELECT ${selectSql} FROM ${quoteIdent(fromTable)}`;
  joins.forEach(j => {
    if (!j.table || !j.leftTable || !j.leftColumn || !j.rightColumn) { sql += ` ${j.type} JOIN …`; return; }
    sql += ` ${j.type} JOIN ${quoteIdent(j.table)} ON ${quoteIdent(j.leftTable)}.${quoteIdent(j.leftColumn)} = ${quoteIdent(j.table)}.${quoteIdent(j.rightColumn)}`;
  });
  if (wheres.length > 0) {
    sql += ' WHERE ' + wheres.map((w, index) => {
      if (!w.table || !w.column || !w.operator) return '…';
      const ref = `${quoteIdent(w.table)}.${quoteIdent(w.column)}`;
      const op = OPERATORS.find(o => o.value === w.operator);
      let fragment;
      if (w.operator === 'is_null') fragment = `${ref} IS NULL`;
      else if (w.operator === 'is_not_null') fragment = `${ref} IS NOT NULL`;
      else if (w.operator === 'contains') fragment = `${ref} LIKE ${quoteLiteral('%' + (w.value || '') + '%')}`;
      else if (w.operator === 'starts_with') fragment = `${ref} LIKE ${quoteLiteral((w.value || '') + '%')}`;
      else if (w.operator === 'ends_with') fragment = `${ref} LIKE ${quoteLiteral('%' + (w.value || ''))}`;
      else fragment = `${ref} ${op ? op.value : w.operator} ${quoteLiteral(w.value)}`;
      if (w.negate) fragment = `NOT (${fragment})`;
      return index === 0 ? fragment : `${w.combinator === 'OR' ? 'OR' : 'AND'} ${fragment}`;
    }).join(' ');
  }
  if (groupBy.length > 0) {
    sql += ' GROUP BY ' + groupBy.map(g => (g.table && g.column ? `${quoteIdent(g.table)}.${quoteIdent(g.column)}` : '…')).join(', ');
  }
  sql += ` LIMIT ${limit}`;
  return sql;
}

// Lets the user assemble a read-only SELECT ... FROM ... JOIN ... WHERE ...
// GROUP BY query visually — pick tables, columns (with optional aliases and
// aggregates), joins, and filters — and run it against the project's own
// SQLite database. The actual SQL is built and validated server-side
// (handlers/database.go's RunQuery) against the real schema and only ever
// emits a SELECT; this component only needs table/column *names* and
// operator choices to build the request, never raw SQL.
export default function QueryBuilderTab({ projectId, tables }) {
  const [fromTable, setFromTable] = useState(undefined);
  const [joins, setJoins] = useState([]);
  const [selects, setSelects] = useState([]);
  const [wheres, setWheres] = useState([]);
  const [groupBy, setGroupBy] = useState([]);
  const [limit, setLimit] = useState(100);
  const [schemaCache, setSchemaCache] = useState({});
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState(null);

  const [nlDescription, setNlDescription] = useState('');
  const [generating, setGenerating] = useState(false);
  const [generateError, setGenerateError] = useState('');
  const [generateNotice, setGenerateNotice] = useState('');

  const ensureSchema = useCallback((table) => {
    if (!table || schemaCache[table]) return;
    fetch(`/api/projects/${projectId}/db/tables/${table}/data?limit=0`)
      .then(res => res.json())
      .then(data => {
        const columns = (data.columns || []).map(col => col.name);
        setSchemaCache(prev => ({ ...prev, [table]: columns }));
      })
      .catch(() => setSchemaCache(prev => ({ ...prev, [table]: [] })));
  }, [projectId, schemaCache]);

  useEffect(() => { ensureSchema(fromTable); }, [fromTable, ensureSchema]);

  // Every table reachable so far — the base table plus each join's own
  // table, in order — is what a select/where/group-by column or a later
  // join's "left table" is allowed to reference. Mirrors the server's own
  // chaining rule.
  const reachableTables = useMemo(() => {
    const list = [];
    if (fromTable) list.push(fromTable);
    joins.forEach(join => { if (join.table) list.push(join.table); });
    return list;
  }, [fromTable, joins]);

  const columnsFor = (table) => schemaCache[table] || [];
  const hasGroupBy = groupBy.length > 0;

  const addJoin = () => setJoins(prev => [...prev, { id: newRowId(), type: 'INNER', table: undefined, leftTable: fromTable, leftColumn: undefined, rightColumn: undefined }]);
  const removeJoin = (id) => setJoins(prev => prev.filter(j => j.id !== id));
  const updateJoin = (id, patch) => setJoins(prev => prev.map(j => (j.id === id ? { ...j, ...patch } : j)));

  const addSelect = () => setSelects(prev => [...prev, { id: newRowId(), table: fromTable, column: undefined, alias: '', aggregate: '' }]);
  const selectAllColumns = (table) => {
    ensureSchema(table);
    const cols = schemaCache[table];
    if (!cols) { setTimeout(() => selectAllColumns(table), 200); return; }
    setSelects(prev => [...prev, ...cols.map(col => ({ id: newRowId(), table, column: col, alias: '', aggregate: '' }))]);
  };
  const removeSelect = (id) => setSelects(prev => prev.filter(s => s.id !== id));
  const updateSelect = (id, patch) => setSelects(prev => prev.map(s => (s.id === id ? { ...s, ...patch } : s)));

  const addWhere = () => setWheres(prev => [...prev, { id: newRowId(), table: fromTable, column: undefined, operator: '=', value: '', negate: false, combinator: 'AND' }]);
  const removeWhere = (id) => setWheres(prev => prev.filter(w => w.id !== id));
  const updateWhere = (id, patch) => setWheres(prev => prev.map(w => (w.id === id ? { ...w, ...patch } : w)));

  const whereFieldOptions = useMemo(() => fieldOptions(reachableTables, columnsFor), [reachableTables, schemaCache]); // eslint-disable-line react-hooks/exhaustive-deps

  const addGroupBy = () => setGroupBy(prev => [...prev, { id: newRowId(), table: fromTable, column: undefined }]);
  const removeGroupBy = (id) => setGroupBy(prev => prev.filter(g => g.id !== id));
  const updateGroupBy = (id, patch) => setGroupBy(prev => prev.map(g => (g.id === id ? { ...g, ...patch } : g)));

  const handleFromChange = (table) => {
    setFromTable(table);
    setJoins([]);
    setWheres([]);
    setGroupBy([]);
    setSelects([{ id: newRowId(), table, column: undefined, alias: '', aggregate: '' }]);
    ensureSchema(table);
  };

  const previewSQL = useMemo(() => buildPreviewSQL({ fromTable, joins, selects, wheres, groupBy, limit }), [fromTable, joins, selects, wheres, groupBy, limit]);

  // Turns the backend's queryRequest JSON (snake_case, no client-side row
  // ids) into this component's row-based state. Conditions using
  // exists/not_exists/in_subquery/not_in_subquery are skipped rather than
  // guessed at — there's no flat-row UI for a nested subquery yet, so
  // silently dropping one would produce a query that looks simpler than
  // what the model actually described; better to say so than misrepresent it.
  const applyGeneratedQuery = (generated) => {
    const skippedSubqueryConditions = (generated.where || []).filter(w =>
      ['exists', 'not_exists', 'in_subquery', 'not_in_subquery'].includes(w.operator)).length;

    setFromTable(generated.from);
    (generated.joins || []).forEach(j => ensureSchema(j.table));
    ensureSchema(generated.from);

    setJoins((generated.joins || []).map(j => ({
      id: newRowId(), type: j.type || 'INNER', table: j.table,
      leftTable: j.left_table, leftColumn: j.left_column, rightColumn: j.right_column,
    })));
    setSelects((generated.select && generated.select.length > 0 ? generated.select : [{}]).map(s => ({
      id: newRowId(), table: s.table || generated.from, column: s.column,
      alias: s.alias || '', aggregate: s.aggregate || '',
    })));
    setWheres((generated.where || [])
      .filter(w => !['exists', 'not_exists', 'in_subquery', 'not_in_subquery'].includes(w.operator))
      .map(w => ({
        id: newRowId(), table: w.table, column: w.column, operator: w.operator,
        value: w.value || '', negate: Boolean(w.negate), combinator: w.combinator || 'AND',
      })));
    setGroupBy((generated.group_by || []).map(g => ({ id: newRowId(), table: g.table, column: g.column })));
    if (generated.limit) setLimit(generated.limit);

    setGenerateNotice(skippedSubqueryConditions > 0
      ? `Filled in the builder below — ${skippedSubqueryConditions} condition(s) used a subquery (EXISTS/IN), which isn't editable in this view yet, so they were left out. Review before running.`
      : 'Filled in the builder below from your description — review before running.');
  };

  const generateFromDescription = () => {
    const description = nlDescription.trim();
    if (!description) return;
    setGenerating(true);
    setGenerateError('');
    setGenerateNotice('');
    fetch(`/api/projects/${projectId}/db/query/generate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ description }),
    })
      .then(async res => {
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data?.error || 'Could not generate a query from that description');
        return data;
      })
      .then(data => applyGeneratedQuery(data.query))
      .catch(err => setGenerateError(err.message || 'Could not generate a query from that description'))
      .finally(() => setGenerating(false));
  };

  const canRun = Boolean(fromTable) && selects.length > 0 && selects.every(s => s.table && s.column)
    && wheres.every(w => w.table && w.column && w.operator && (NO_VALUE_OPERATORS.has(w.operator) || w.value !== ''));

  const runQuery = () => {
    setRunning(true);
    setError('');
    setResult(null);
    const payload = {
      from: fromTable,
      select: selects.map(s => ({ table: s.table, column: s.column, alias: s.alias || undefined, aggregate: s.aggregate || undefined })),
      joins: joins.map(j => ({ type: j.type, table: j.table, left_table: j.leftTable, left_column: j.leftColumn, right_column: j.rightColumn })),
      where: wheres.map(w => ({
        table: w.table, column: w.column, operator: w.operator,
        value: NO_VALUE_OPERATORS.has(w.operator) ? undefined : w.value,
        negate: w.negate || undefined,
        combinator: w.combinator && w.combinator !== 'AND' ? w.combinator : undefined,
      })),
      group_by: groupBy.filter(g => g.table && g.column).map(g => ({ table: g.table, column: g.column })),
      limit,
    };
    fetch(`/api/projects/${projectId}/db/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
      .then(async res => {
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data?.error || 'Query failed');
        return data;
      })
      .then(data => setResult(data))
      .catch(err => setError(err.message || 'Query failed'))
      .finally(() => setRunning(false));
  };

  const resultColumns = (result?.columns || []).map(name => ({
    title: name,
    dataIndex: name,
    key: name,
    ellipsis: true,
    render: text => (typeof text === 'object' ? JSON.stringify(text) : String(text ?? '')),
  }));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', gap: 16, overflowY: 'auto', paddingRight: 4 }}>
      <div className="db-premium-table" style={{ padding: 20, display: 'flex', flexDirection: 'column', gap: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <ApartmentOutlined style={{ fontSize: 18, color: 'var(--studio-accent, #c15f3c)' }} />
          <Title level={5} style={{ margin: 0 }}>Query Builder</Title>
        </div>

        {/* Describe it in plain English, fill in the builder below */}
        <div>
          <Text strong style={{ display: 'block', marginBottom: 6 }}>Describe what you want</Text>
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder='e.g. "authors who have written more than one book"'
              value={nlDescription}
              onChange={e => setNlDescription(e.target.value)}
              onPressEnter={generateFromDescription}
              disabled={generating}
            />
            <Button type="primary" icon={<ThunderboltOutlined />} loading={generating}
              disabled={!nlDescription.trim()} onClick={generateFromDescription}>
              Generate
            </Button>
          </Space.Compact>
          {generateError && <Alert style={{ marginTop: 8 }} type="error" showIcon message={generateError} closable onClose={() => setGenerateError('')} />}
          {generateNotice && <Alert style={{ marginTop: 8 }} type="info" showIcon message={generateNotice} closable onClose={() => setGenerateNotice('')} />}
        </div>

        <Divider style={{ margin: '4px 0' }} />

        {/* FROM */}
        <div>
          <Text strong style={{ display: 'block', marginBottom: 6 }}>From table</Text>
          <Select
            style={{ width: 280 }}
            placeholder="Choose a table"
            options={tables.map(t => ({ value: t, label: t }))}
            value={fromTable}
            onChange={handleFromChange}
            showSearch
          />
        </div>

        {/* JOINS */}
        <div>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
            <Text strong>Joins</Text>
            <Button size="small" icon={<PlusOutlined />} onClick={addJoin} disabled={!fromTable}>Add join</Button>
          </div>
          {joins.length === 0 ? (
            <Text type="secondary" style={{ fontSize: 12 }}>No joins — querying a single table.</Text>
          ) : (
            <Space direction="vertical" style={{ width: '100%' }} size={8}>
              {joins.map(join => (
                <Space key={join.id} wrap size={8} style={{ background: 'var(--studio-panel, #f5f1e9)', padding: 8, borderRadius: 8 }}>
                  <Select size="small" style={{ width: 110 }} options={JOIN_TYPES} value={join.type}
                    onChange={value => updateJoin(join.id, { type: value })} />
                  <Select size="small" style={{ width: 160 }} placeholder="join table" showSearch
                    options={tables.filter(t => !reachableTables.includes(t)).map(t => ({ value: t, label: t }))}
                    value={join.table}
                    onChange={value => { updateJoin(join.id, { table: value, rightColumn: undefined }); ensureSchema(value); }} />
                  <Text type="secondary" style={{ fontSize: 12 }}>ON</Text>
                  <Select size="small" style={{ width: 140 }} placeholder="left table" showSearch
                    options={reachableTables.filter(t => t !== join.table).map(t => ({ value: t, label: t }))}
                    value={join.leftTable}
                    onChange={value => updateJoin(join.id, { leftTable: value, leftColumn: undefined })} />
                  <Select size="small" style={{ width: 140 }} placeholder="column" showSearch
                    options={columnsFor(join.leftTable).map(c => ({ value: c, label: c }))}
                    value={join.leftColumn}
                    onChange={value => updateJoin(join.id, { leftColumn: value })} />
                  <Text type="secondary" style={{ fontSize: 12 }}>=</Text>
                  <Select size="small" style={{ width: 140 }} placeholder="column" showSearch
                    options={columnsFor(join.table).map(c => ({ value: c, label: c }))}
                    value={join.rightColumn}
                    onChange={value => updateJoin(join.id, { rightColumn: value })} />
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => removeJoin(join.id)} />
                </Space>
              ))}
            </Space>
          )}
        </div>

        {/* SELECT columns */}
        <div>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
            <Text strong>Columns to select</Text>
            <Space size={8}>
              {fromTable && (
                <Tooltip title={`Add every column of ${fromTable} (like SELECT *)`}>
                  <Button size="small" icon={<SelectOutlined />} onClick={() => selectAllColumns(fromTable)}>Select all from {fromTable}</Button>
                </Tooltip>
              )}
              <Button size="small" icon={<PlusOutlined />} onClick={addSelect} disabled={!fromTable}>Add column</Button>
            </Space>
          </div>
          {selects.length === 0 ? (
            <Text type="secondary" style={{ fontSize: 12 }}>Pick a "From" table to start choosing columns.</Text>
          ) : (
            <Space direction="vertical" style={{ width: '100%' }} size={8}>
              {selects.map(sel => (
                <Space key={sel.id} wrap size={8}>
                  <Select size="small" style={{ width: 150 }} placeholder="table" showSearch
                    options={reachableTables.map(t => ({ value: t, label: t }))}
                    value={sel.table}
                    onChange={value => updateSelect(sel.id, { table: value, column: undefined })} />
                  <Select size="small" style={{ width: 150 }} placeholder="column" showSearch
                    options={columnsFor(sel.table).map(c => ({ value: c, label: c }))}
                    value={sel.column}
                    onChange={value => updateSelect(sel.id, { column: value })} />
                  {hasGroupBy && (
                    <Select size="small" style={{ width: 130 }} options={AGGREGATES} value={sel.aggregate}
                      onChange={value => updateSelect(sel.id, { aggregate: value })} />
                  )}
                  <Input size="small" style={{ width: 130 }} placeholder="alias (optional)"
                    value={sel.alias} onChange={e => updateSelect(sel.id, { alias: e.target.value })} />
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => removeSelect(sel.id)} />
                </Space>
              ))}
            </Space>
          )}
        </div>

        {/* WHERE */}
        <div>
          <Text strong style={{ display: 'block', marginBottom: 8 }}>Filters</Text>
          {wheres.length === 0 ? (
            <Text type="secondary" style={{ fontSize: 12 }}>No filters — every row is included.</Text>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 8 }}>
              {wheres.map((w, index) => (
                <div key={w.id} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  {index > 0 && (
                    <button
                      type="button"
                      title="Toggle AND / OR — combines with the condition above"
                      onClick={() => updateWhere(w.id, { combinator: w.combinator === 'OR' ? 'AND' : 'OR' })}
                      style={{
                        fontSize: 11, fontWeight: 700, cursor: 'pointer', border: 'none',
                        color: w.combinator === 'OR' ? 'var(--studio-accent, #c15f3c)' : 'var(--studio-muted, #746b63)',
                        background: w.combinator === 'OR' ? 'color-mix(in srgb, var(--studio-accent, #c15f3c) 12%, transparent)' : 'var(--studio-panel, #eee9df)',
                        borderRadius: 999, padding: '2px 10px', flexShrink: 0,
                      }}
                    >
                      {w.combinator === 'OR' ? 'OR' : 'AND'}
                    </button>
                  )}
                  <Tooltip title="Negate this condition (NOT)">
                    <Button
                      size="small"
                      type={w.negate ? 'primary' : 'default'}
                      danger={w.negate}
                      icon={<StopOutlined />}
                      onClick={() => updateWhere(w.id, { negate: !w.negate })}
                      style={{ flexShrink: 0 }}
                    />
                  </Tooltip>
                  <div className="qb-filter-row">
                    <Select
                      className="qb-filter-field"
                      placeholder="Field"
                      showSearch
                      options={whereFieldOptions}
                      value={w.table && w.column ? `${w.table}${FIELD_SEP}${w.column}` : undefined}
                      onChange={value => {
                        const [table, column] = value.split(FIELD_SEP);
                        updateWhere(w.id, { table, column });
                      }}
                      variant="borderless"
                    />
                    <Dropdown
                      trigger={['click']}
                      menu={{
                        items: OPERATORS.map(op => ({ key: op.value, label: op.label })),
                        selectedKeys: [w.operator],
                        onClick: ({ key }) => updateWhere(w.id, { operator: key }),
                      }}
                    >
                      <button type="button" className="qb-operator-btn" title="Change operator">
                        {OPERATOR_GLYPHS[w.operator] || w.operator}
                      </button>
                    </Dropdown>
                    {NO_VALUE_OPERATORS.has(w.operator) ? (
                      <span className="qb-filter-value qb-filter-value-empty">no value needed</span>
                    ) : (
                      <Input
                        className="qb-filter-value"
                        placeholder="Value"
                        value={w.value}
                        onChange={e => updateWhere(w.id, { value: e.target.value })}
                        variant="borderless"
                      />
                    )}
                  </div>
                  <Button type="text" icon={<DeleteOutlined />} onClick={() => removeWhere(w.id)}
                    style={{ color: 'var(--studio-subtle, #91877e)', flexShrink: 0 }} />
                </div>
              ))}
            </div>
          )}
          <Button type="link" icon={<PlusOutlined />} onClick={addWhere} disabled={!fromTable} style={{ paddingLeft: 0 }}>
            Add condition
          </Button>
        </div>

        {/* GROUP BY */}
        <div>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
            <Text strong>Group by</Text>
            <Button size="small" icon={<PlusOutlined />} onClick={addGroupBy} disabled={!fromTable}>Add group-by column</Button>
          </div>
          {groupBy.length === 0 ? (
            <Text type="secondary" style={{ fontSize: 12 }}>No grouping — one row per match. Add a group-by column to unlock aggregates (COUNT, SUM, …) on the select columns above.</Text>
          ) : (
            <Space direction="vertical" style={{ width: '100%' }} size={8}>
              {groupBy.map(g => (
                <Space key={g.id} wrap size={8}>
                  <Select size="small" style={{ width: 150 }} placeholder="table" showSearch
                    options={reachableTables.map(t => ({ value: t, label: t }))}
                    value={g.table}
                    onChange={value => updateGroupBy(g.id, { table: value, column: undefined })} />
                  <Select size="small" style={{ width: 150 }} placeholder="column" showSearch
                    options={columnsFor(g.table).map(c => ({ value: c, label: c }))}
                    value={g.column}
                    onChange={value => updateGroupBy(g.id, { column: value })} />
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => removeGroupBy(g.id)} />
                </Space>
              ))}
            </Space>
          )}
        </div>

        <Divider style={{ margin: '4px 0' }} />

        {/* Live SQL preview */}
        {previewSQL && (
          <div style={{ background: 'var(--studio-panel, #f5f1e9)', borderRadius: 8, padding: '10px 12px', overflowX: 'auto' }}>
            <Text code style={{ fontSize: 12, whiteSpace: 'pre' }}>{previewSQL}</Text>
          </div>
        )}

        <Space align="center">
          <Text strong>Limit</Text>
          <InputNumber min={1} max={500} value={limit} onChange={v => setLimit(v || 100)} style={{ width: 100 }} />
          <Button type="primary" icon={<PlayCircleOutlined />} loading={running} disabled={!canRun} onClick={runQuery}>
            Run query
          </Button>
        </Space>
      </div>

      {error && <Alert type="error" showIcon message={error} closable onClose={() => setError('')} />}

      {result && (
        <div className="db-premium-table" style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
          <div style={{ padding: '10px 16px', borderBottom: '1px solid var(--studio-border, #e2dcd4)', overflowX: 'auto' }}>
            <Text code style={{ fontSize: 12, whiteSpace: 'pre' }}>{result.sql}</Text>
          </div>
          <Table
            dataSource={result.rows}
            columns={resultColumns}
            rowKey={(record, idx) => idx}
            scroll={{ x: 'max-content', y: 'calc(100vh - 640px)' }}
            locale={{ emptyText: <Empty description="No rows matched." /> }}
            pagination={false}
            size="middle"
            style={{ flex: 1 }}
          />
        </div>
      )}
    </div>
  );
}
