export const folderNameFromPath = (path) => path?.split(/[\\/]/).filter(Boolean).pop() || 'Workspace';
