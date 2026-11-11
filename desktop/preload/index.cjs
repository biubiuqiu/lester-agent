const { contextBridge, ipcRenderer } = require('electron');

const invoke = async (action, ...args) => {
  const result = await ipcRenderer.invoke('lester:desktop', { action, args });
  if (!result?.ok) {
    const error = new Error(result?.error?.message || 'The desktop request failed.');
    error.code = result?.error?.code || 'DESKTOP_REQUEST_FAILED';
    throw error;
  }
  return result.value;
};
contextBridge.exposeInMainWorld('lesterDesktop', Object.freeze({
  getState: () => invoke('getState'),
  chooseProject: () => invoke('chooseProject'),
  request: (method, params = {}) => invoke('request', method, params),
  saveModel: model => invoke('saveModel', model),
  deleteModel: id => invoke('deleteModel', id),
  configureCloud: origin => invoke('configureCloud', origin),
  cloudLogin: () => invoke('cloudLogin'),
  cloudLogout: () => invoke('cloudLogout'),
  cloudRequest: (method, params = {}) => invoke('cloudRequest', method, params),
  openCloudConversation: (id, bounds) => invoke('openCloudConversation', id, bounds),
  resizeCloudConversation: bounds => invoke('resizeCloudConversation', bounds),
  closeCloudConversation: () => invoke('closeCloudConversation'),
  openExternal: url => invoke('openExternal', url),
  onEvent: callback => {
    if (typeof callback !== 'function') throw new TypeError('Expected an event callback.');
    const listener = (_event, payload) => callback(payload);
    ipcRenderer.on('lester:event', listener);
    return () => ipcRenderer.removeListener('lester:event', listener);
  },
}));
