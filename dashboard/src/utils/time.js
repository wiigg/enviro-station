const convertEpochToLocal = (epochTime) => {
  const time = parseInt(epochTime);
  const date = new Date(time * 1000);
  const hours = date.getHours();
  const minutes = "0" + date.getMinutes();
  const seconds = "0" + date.getSeconds();
  return `${hours}:${minutes.slice(-2)}:${seconds.slice(-2)}`;
};

export default convertEpochToLocal;
