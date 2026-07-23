import React, { useCallback } from 'react';

export default function DragHandle({ onDrag, onStart, direction = 'vertical' }) {
  const isVertical = direction === 'vertical';

  const onMouseDown = useCallback((e) => {
    e.preventDefault();
    if (onStart) onStart();
    const startPos = isVertical ? e.clientX : e.clientY;

    const onMouseMove = (moveEvt) => {
      const currentPos = isVertical ? moveEvt.clientX : moveEvt.clientY;
      onDrag(currentPos, startPos);
    };

    const onMouseUp = () => {
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.body.style.cursor = isVertical ? 'col-resize' : 'row-resize';
    document.body.style.userSelect = 'none';
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  }, [onDrag, onStart, isVertical]);

  return (
    <div
      onMouseDown={onMouseDown}
      style={{
        ...(isVertical
          ? { width: 4, cursor: 'col-resize' }
          : { height: 4, cursor: 'row-resize' }),
        backgroundColor: 'var(--studio-border, #d8d1c5)',
        flexShrink: 0,
        transition: 'background-color 0.15s',
        zIndex: 10,
      }}
      onMouseOver={(e) => e.currentTarget.style.backgroundColor = 'var(--studio-accent, #c15f3c)'}
      onMouseOut={(e) => e.currentTarget.style.backgroundColor = 'var(--studio-border, #d8d1c5)'}
    />
  );
}
