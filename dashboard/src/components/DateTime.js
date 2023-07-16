import { useEffect, useState } from "react";

const DateTime = () => {
  const [date, setDate] = useState(new Date());

  useEffect(() => {
    let timer = setInterval(() => setDate(new Date()), 1000);

    return function cleanup() {
      clearInterval(timer);
    };
  });

  const dateOptions = { year: "numeric", month: "long", day: "numeric" };
  const timeOptions = { hour: "2-digit", minute: "2-digit" };

  return (
    <div>
      {date.toLocaleDateString(undefined, dateOptions)},{" "}
      {date.toLocaleTimeString(undefined, timeOptions)}
    </div>
  );
};

export default DateTime;
