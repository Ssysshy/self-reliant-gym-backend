-- MySQL dump 10.13  Distrib 9.6.0, for macos26.2 (arm64)
--
-- Host: 127.0.0.1    Database: gym
-- ------------------------------------------------------
-- Server version	9.6.0

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;
SET @MYSQLDUMP_TEMP_LOG_BIN = @@SESSION.SQL_LOG_BIN;
SET @@SESSION.SQL_LOG_BIN= 0;

--
-- GTID state at the beginning of the backup 
--

SET @@GLOBAL.GTID_PURGED=/*!80000 '+'*/ 'd7378d88-39c2-11f1-9e51-2ca7c2d5e5fb:1-12734';

--
-- Dumping data for table `plans`
--
-- WHERE:  owner=0

LOCK TABLES `plans` WRITE;
/*!40000 ALTER TABLE `plans` DISABLE KEYS */;
INSERT INTO `plans` VALUES (1,0,'全身入门','[{\"Focus\": \"全身\", \"Items\": [{\"Name\": \"杯式深蹲\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿推胸\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿划船\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 2}, {\"Focus\": \"全身\", \"Items\": [{\"Name\": \"腿举\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"高位下拉\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"器械推肩\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 5}]',1,1,NULL),(2,0,'上肢／下肢／全身','[{\"Focus\": \"上肢\", \"Items\": [{\"Name\": \"坐姿推胸\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿划船\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 1}, {\"Focus\": \"下肢\", \"Items\": [{\"Name\": \"腿举\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿腿弯举\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 3}, {\"Focus\": \"全身\", \"Items\": [{\"Name\": \"杯式深蹲\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"高位下拉\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿推胸\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 6}]',1,1,NULL),(3,0,'上下肢交替','[{\"Focus\": \"上肢\", \"Items\": [{\"Name\": \"坐姿推胸\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿划船\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 1}, {\"Focus\": \"下肢\", \"Items\": [{\"Name\": \"腿举\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"坐姿腿弯举\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 2}, {\"Focus\": \"上肢\", \"Items\": [{\"Name\": \"高位下拉\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"器械推肩\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 4}, {\"Focus\": \"下肢\", \"Items\": [{\"Name\": \"杯式深蹲\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}, {\"Name\": \"臀桥\", \"Reps\": 10, \"Rest\": 60, \"Sets\": 3}], \"Weekday\": 6}]',1,1,NULL);
/*!40000 ALTER TABLE `plans` ENABLE KEYS */;
UNLOCK TABLES;
SET @@SESSION.SQL_LOG_BIN = @MYSQLDUMP_TEMP_LOG_BIN;
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2026-11-12 14:09:03
